package node

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	"github.com/pkg/errors"

	session "go-wrapper/go-dkls/sessions"

	"github.com/pushchain/push-chain-node/universalClient/tss"
	"github.com/pushchain/push-chain-node/universalClient/tss/dkls"
)

// KeygenRequest contains parameters for a keygen operation.
type KeygenRequest struct {
	EventID       string
	BlockNumber   uint64
	Participants  []*tss.UniversalValidator
	IsCoordinator bool
}

// KeygenResult contains the result of a keygen operation.
type KeygenResult struct {
	Keyshare     []byte
	Participants []string
}

// executeKeygen executes a keygen operation by coordinating dkls session and networking.
func (n *Node) executeKeygen(ctx context.Context, req KeygenRequest) (*KeygenResult, error) {
	n.logger.Info().
		Str("event_id", req.EventID).
		Bool("coordinator", req.IsCoordinator).
		Int("participants", len(req.Participants)).
		Msg("starting keygen")

	// Sort participants by party ID for consistency
	participants := make([]*tss.UniversalValidator, len(req.Participants))
	copy(participants, req.Participants)
	sort.Slice(participants, func(i, j int) bool {
		return participants[i].PartyID() < participants[j].PartyID()
	})

	// Extract party IDs
	partyIDs := make([]string, len(participants))
	partyIDToValidator := make(map[string]*tss.UniversalValidator)
	for i, p := range participants {
		partyID := p.PartyID()
		partyIDs[i] = partyID
		partyIDToValidator[partyID] = p
	}

	// Calculate threshold
	threshold := calculateThreshold(len(participants))

	// Encode participant IDs for setup message
	participantIDs := make([]byte, 0, len(partyIDs)*10)
	for i, partyID := range partyIDs {
		if i > 0 {
			participantIDs = append(participantIDs, 0) // Separator
		}
		participantIDs = append(participantIDs, []byte(partyID)...)
	}

	// Ensure all peers are registered before starting
	for _, p := range participants {
		if p.PartyID() == n.validatorAddress {
			continue // Skip self
		}
		if err := n.network.EnsurePeer(p.Network.PeerID, p.Network.Multiaddrs); err != nil {
			return nil, errors.Wrapf(err, "failed to ensure peer %s", p.PartyID())
		}
	}

	// Setup waiting mechanism
	setupCh := make(chan []byte, 1)
	setupReceived := false
	var setupData []byte

	// Register setup handler for this event
	n.setupHandlersMu.Lock()
	n.setupHandlers[req.EventID] = func(data []byte) {
		select {
		case setupCh <- data:
		default:
		}
	}
	n.setupHandlersMu.Unlock()

	// Cleanup setup handler when done
	defer func() {
		n.setupHandlersMu.Lock()
		delete(n.setupHandlers, req.EventID)
		n.setupHandlersMu.Unlock()
	}()

	// Coordinator creates and broadcasts setup
	if req.IsCoordinator {
		n.logger.Info().
			Str("event_id", req.EventID).
			Msg("coordinator creating and broadcasting setup")

		// Give other nodes time to register their setup handlers
		time.Sleep(2 * time.Second)

		// Create setup message
		var err error
		setupData, err = session.DklsKeygenSetupMsgNew(threshold, nil, participantIDs)
		if err != nil {
			return nil, errors.Wrap(err, "failed to create keygen setup")
		}

		// Broadcast setup to all participants
		setupMsg := map[string]interface{}{
			"type":      "setup",
			"event_id":  req.EventID,
			"setup":     setupData,
			"threshold": threshold,
		}
		setupPayload, err := json.Marshal(setupMsg)
		if err != nil {
			return nil, errors.Wrap(err, "failed to marshal setup message")
		}

		// Send to all participants
		for _, p := range participants {
			if p.PartyID() == n.validatorAddress {
				continue // Skip self
			}
			if err := n.network.Send(ctx, p.Network.PeerID, setupPayload); err != nil {
				n.logger.Warn().
					Err(err).
					Str("receiver", p.PartyID()).
					Msg("failed to send setup to participant")
				// Continue - other participants may still receive it
			} else {
				n.logger.Debug().
					Str("receiver", p.PartyID()).
					Msg("sent setup to participant")
			}
		}

		// Coordinator uses its own setup
		setupReceived = true
	} else {
		// Participant waits for setup from coordinator
		n.logger.Info().
			Str("event_id", req.EventID).
			Msg("participant waiting for setup from coordinator")

		timeout := time.NewTimer(30 * time.Second)
		defer timeout.Stop()

		select {
		case setupData = <-setupCh:
			setupReceived = true
			n.logger.Info().
				Str("event_id", req.EventID).
				Msg("received setup from coordinator")
		case <-timeout.C:
			// Timeout - create setup ourselves (deterministic fallback)
			n.logger.Warn().
				Str("event_id", req.EventID).
				Msg("setup timeout, creating setup deterministically")
			var err error
			setupData, err = session.DklsKeygenSetupMsgNew(threshold, nil, participantIDs)
			if err != nil {
				return nil, errors.Wrap(err, "failed to create keygen setup (fallback)")
			}
			setupReceived = true
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	if !setupReceived || len(setupData) == 0 {
		return nil, errors.New("failed to obtain setup data")
	}

	// Create DKLS session with setup
	n.logger.Debug().
		Str("event_id", req.EventID).
		Int("setup_len", len(setupData)).
		Int("threshold", threshold).
		Int("participants", len(partyIDs)).
		Msg("creating DKLS keygen session")

	dklsSession, err := dkls.NewKeygenSession(
		setupData,
		req.EventID,
		n.validatorAddress,
		partyIDs,
		threshold,
	)
	if err != nil {
		n.logger.Error().
			Err(err).
			Str("event_id", req.EventID).
			Int("setup_len", len(setupData)).
			Msg("failed to create keygen session")
		return nil, errors.Wrap(err, "failed to create keygen session")
	}
	defer dklsSession.Close()

	// Register session for message routing
	n.sessionsMu.Lock()
	n.sessions[req.EventID] = dklsSession
	n.sessionsMu.Unlock()

	// Ensure session is removed when done
	defer func() {
		n.sessionsMu.Lock()
		delete(n.sessions, req.EventID)
		n.sessionsMu.Unlock()
	}()

	// Execute protocol - matching reference implementation pattern:
	// 1. Get outputs (from session creation)
	// 2. Loop: if no outputs, block waiting for input → process input → get outputs → send → repeat
	// This matches the reference: OutputMessage() → if empty, block on channel → InputMessage() → repeat

	n.logger.Info().
		Str("event_id", req.EventID).
		Msg("starting protocol execution")

	// Get initial outputs from session creation
	initialMessages, _, err := dklsSession.Step()
	if err != nil {
		return nil, errors.Wrap(err, "failed to get initial outputs")
	}

	// Send initial outputs
	for _, msg := range initialMessages {
		validator, ok := partyIDToValidator[msg.Receiver]
		if !ok {
			n.logger.Warn().Str("receiver", msg.Receiver).Msg("unknown receiver party ID")
			continue
		}
		if err := n.network.Send(ctx, validator.Network.PeerID, msg.Data); err != nil {
			return nil, errors.Wrapf(err, "failed to send initial message to %s", msg.Receiver)
		}
		n.logger.Info().
			Str("event_id", req.EventID).
			Str("receiver", msg.Receiver).
			Int("msg_len", len(msg.Data)).
			Msg("sent initial protocol message")
	}

	// Main protocol loop - simple: process queued messages and send outputs
	// Messages arrive asynchronously via InputMessage() and get queued
	// Step() processes one queued message and returns outputs immediately
	sessionFinished := false
	maxIterations := 1000

	for iteration := 0; iteration < maxIterations; iteration++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		// Step() processes one queued message (if available) and returns outputs
		messages, finished, err := dklsSession.Step()
		if err != nil {
			return nil, errors.Wrap(err, "session step failed")
		}

		if finished {
			sessionFinished = true
			n.logger.Info().
				Str("event_id", req.EventID).
				Msg("protocol finished")
		}

		// Send output messages immediately
		for _, msg := range messages {
			validator, ok := partyIDToValidator[msg.Receiver]
			if !ok {
				n.logger.Warn().Str("receiver", msg.Receiver).Msg("unknown receiver party ID")
				continue
			}
			if err := n.network.Send(ctx, validator.Network.PeerID, msg.Data); err != nil {
				return nil, errors.Wrapf(err, "failed to send message to %s", msg.Receiver)
			}
			n.logger.Info().
				Str("event_id", req.EventID).
				Str("receiver", msg.Receiver).
				Int("msg_len", len(msg.Data)).
				Msg("sent protocol message")
		}

		// If finished and no more messages, we're done
		if sessionFinished && len(messages) == 0 {
			break
		}

		// If no messages to send, wait a bit for new messages to arrive
		if len(messages) == 0 {
			time.Sleep(50 * time.Millisecond)
		}
	}

	if !sessionFinished {
		return nil, errors.Errorf("protocol did not finish after %d iterations", maxIterations)
	}

	// Get result
	result, err := dklsSession.GetResult()
	if err != nil {
		return nil, errors.Wrap(err, "failed to get keygen result")
	}

	n.logger.Info().
		Str("event_id", req.EventID).
		Int("keyshare_len", len(result.Keyshare)).
		Msg("keygen completed successfully")

	return &KeygenResult{
		Keyshare:     result.Keyshare,
		Participants: result.Participants,
	}, nil
}
