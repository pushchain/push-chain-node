package sessionmanager

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"sync"

	"github.com/pkg/errors"
	"github.com/rs/zerolog"

	"github.com/pushchain/push-chain-node/universalClient/store"
	"github.com/pushchain/push-chain-node/universalClient/tss/coordinator"
	"github.com/pushchain/push-chain-node/universalClient/tss/dkls"
	"github.com/pushchain/push-chain-node/universalClient/tss/eventstore"
	"github.com/pushchain/push-chain-node/universalClient/tss/keyshare"
)

// SendFunc is a function type for sending messages to participants.
type SendFunc func(ctx context.Context, peerID string, data []byte) error

// SessionManager manages TSS protocol sessions and handles incoming messages.
type SessionManager struct {
	eventStore      *eventstore.Store
	coordinator     *coordinator.Coordinator
	keyshareManager *keyshare.Manager
	send            SendFunc
	partyID         string // Our validator address
	logger          zerolog.Logger

	// Session storage
	mu       sync.RWMutex
	sessions map[string]dkls.Session // eventID -> Session
}

// NewSessionManager creates a new session manager.
func NewSessionManager(
	eventStore *eventstore.Store,
	coord *coordinator.Coordinator,
	keyshareManager *keyshare.Manager,
	send SendFunc,
	partyID string,
	logger zerolog.Logger,
) *SessionManager {
	return &SessionManager{
		eventStore:      eventStore,
		coordinator:     coord,
		keyshareManager: keyshareManager,
		send:            send,
		partyID:         partyID,
		logger:          logger,
		sessions:        make(map[string]dkls.Session),
	}
}

// HandleIncomingMessage handles an incoming message.
// peerID: The peer ID of the sender
// data: The raw message bytes (should be JSON-encoded coordinator.Message)
func (sm *SessionManager) HandleIncomingMessage(ctx context.Context, peerID string, data []byte) error {
	// Unmarshal message
	var msg coordinator.Message
	if err := json.Unmarshal(data, &msg); err != nil {
		return errors.Wrap(err, "failed to unmarshal message")
	}

	sm.logger.Debug().
		Str("peer_id", peerID).
		Str("type", msg.Type).
		Str("event_id", msg.EventID).
		Int("participants_count", len(msg.Participants)).
		Msg("handling incoming message")

	// Route based on message type
	switch msg.Type {
	case "setup":
		return sm.handleSetupMessage(ctx, peerID, &msg)
	case "step":
		return sm.handleStepMessage(ctx, peerID, &msg)
	default:
		return errors.Errorf("unknown message type: %s", msg.Type)
	}
}

// handleSetupMessage validates and processes a setup message.
func (sm *SessionManager) handleSetupMessage(ctx context.Context, senderPeerID string, msg *coordinator.Message) error {
	// 1. Validate event exists in DB
	event, err := sm.eventStore.GetEvent(msg.EventID)
	if err != nil {
		return errors.Wrapf(err, "event %s not found in database", msg.EventID)
	}

	// 2. Validate sender is coordinator
	isCoord, err := sm.coordinator.IsPeerCoordinator(ctx, senderPeerID)
	if err != nil {
		return errors.Wrap(err, "failed to check if sender is coordinator")
	}
	if !isCoord {
		return errors.Errorf("sender %s is not the coordinator", senderPeerID)
	}

	// 3. Validate participants list matches event protocol requirements
	if err := sm.validateParticipants(msg.Participants, event); err != nil {
		return errors.Wrap(err, "participants validation failed")
	}

	// 4. Check if session already exists
	sm.mu.Lock()
	if _, exists := sm.sessions[msg.EventID]; exists {
		sm.mu.Unlock()
		sm.logger.Warn().Str("event_id", msg.EventID).Msg("session already exists, ignoring setup")
		return nil
	}
	sm.mu.Unlock()

	// 5. Create session based on protocol type
	session, err := sm.createSession(ctx, event, msg)
	if err != nil {
		return errors.Wrapf(err, "failed to create session for event %s", msg.EventID)
	}

	// 6. Store session
	sm.mu.Lock()
	sm.sessions[msg.EventID] = session
	sm.mu.Unlock()

	// 7. Update event status to IN_PROGRESS
	if err := sm.eventStore.UpdateStatus(msg.EventID, eventstore.StatusInProgress, ""); err != nil {
		sm.logger.Warn().Err(err).Str("event_id", msg.EventID).Msg("failed to update event status")
	}

	sm.logger.Info().
		Str("event_id", msg.EventID).
		Str("protocol", event.ProtocolType).
		Msg("created session from setup message")

	// 8. Process initial step to get output messages
	return sm.processSessionStep(ctx, msg.EventID)
}

// handleStepMessage validates and processes a step message.
func (sm *SessionManager) handleStepMessage(ctx context.Context, senderPeerID string, msg *coordinator.Message) error {
	// 1. Get session
	sm.mu.RLock()
	session, exists := sm.sessions[msg.EventID]
	sm.mu.RUnlock()

	if !exists {
		return errors.Errorf("session for event %s does not exist", msg.EventID)
	}

	// 2. Validate sender is from session participants
	// Get participants from session
	sessionParticipants := session.GetParticipants()

	// Get sender's validator address from peerID
	senderPartyID, err := sm.coordinator.GetPartyIDFromPeerID(ctx, senderPeerID)
	if err != nil {
		return errors.Wrapf(err, "failed to get partyID for sender peerID %s", senderPeerID)
	}

	// Check if sender is in participants
	isParticipant := false
	for _, p := range sessionParticipants {
		if p == senderPartyID {
			isParticipant = true
			break
		}
	}
	if !isParticipant {
		return errors.Errorf("sender %s (partyID: %s) is not in session participants for event %s", senderPeerID, senderPartyID, msg.EventID)
	}

	// 3. Route message to session
	if err := session.InputMessage(msg.Payload); err != nil {
		return errors.Wrapf(err, "failed to input message to session %s", msg.EventID)
	}

	// 4. Process step
	return sm.processSessionStep(ctx, msg.EventID)
}

// processSessionStep processes a step for the given session and sends output messages.
func (sm *SessionManager) processSessionStep(ctx context.Context, eventID string) error {
	sm.mu.RLock()
	session, exists := sm.sessions[eventID]
	sm.mu.RUnlock()

	if !exists {
		return errors.Errorf("session for event %s does not exist", eventID)
	}

	// Step the session
	messages, finished, err := session.Step()
	if err != nil {
		return errors.Wrapf(err, "failed to step session %s", eventID)
	}

	// Send output messages
	for _, dklsMsg := range messages {
		// Find peerID for receiver partyID
		peerID, err := sm.coordinator.GetPeerIDFromPartyID(ctx, dklsMsg.Receiver)
		if err != nil {
			sm.logger.Warn().
				Err(err).
				Str("receiver_party_id", dklsMsg.Receiver).
				Msg("failed to get peerID for receiver")
			continue
		}

		// Create coordinator message
		coordMsg := coordinator.Message{
			Type:         "step",
			EventID:      eventID,
			Payload:      dklsMsg.Data,
			Participants: nil, // Participants not needed for step messages
		}
		msgBytes, err := json.Marshal(coordMsg)
		if err != nil {
			sm.logger.Warn().Err(err).Msg("failed to marshal step message")
			continue
		}

		// Send message
		if err := sm.send(ctx, peerID, msgBytes); err != nil {
			sm.logger.Warn().
				Err(err).
				Str("receiver", dklsMsg.Receiver).
				Str("peer_id", peerID).
				Msg("failed to send step message")
			continue
		}

		sm.logger.Debug().
			Str("event_id", eventID).
			Str("receiver", dklsMsg.Receiver).
			Msg("sent step message")
	}

	// If finished, handle result
	if finished {
		return sm.handleSessionFinished(ctx, eventID, session)
	}

	return nil
}

// handleSessionFinished handles a completed session.
func (sm *SessionManager) handleSessionFinished(ctx context.Context, eventID string, session dkls.Session) error {
	// Ensure session is cleaned up even on error
	defer func() {
		sm.mu.Lock()
		delete(sm.sessions, eventID)
		sm.mu.Unlock()
		session.Close()
		sm.logger.Info().Str("event_id", eventID).Msg("session cleaned up")
	}()

	// Get result
	result, err := session.GetResult()
	if err != nil {
		return errors.Wrapf(err, "failed to get result for session %s", eventID)
	}

	// Get session type
	sessionType := session.GetType()

	// Handle based on session type
	switch sessionType {
	case dkls.SessionTypeKeygen:
		// Save keyshare using keyID from result
		if err := sm.keyshareManager.Store(result.Keyshare, result.KeyID); err != nil {
			return errors.Wrapf(err, "failed to store keyshare for event %s", eventID)
		}
		sm.logger.Info().
			Str("event_id", eventID).
			Str("key_id", result.KeyID).
			Int("public_key_len", len(result.PublicKey)).
			Msg("saved keyshare from keygen")

	case dkls.SessionTypeKeyrefresh:
		// Save new keyshare using keyID from result
		if err := sm.keyshareManager.Store(result.Keyshare, result.KeyID); err != nil {
			return errors.Wrapf(err, "failed to store keyshare for event %s", eventID)
		}
		sm.logger.Info().
			Str("event_id", eventID).
			Str("new_key_id", result.KeyID).
			Int("public_key_len", len(result.PublicKey)).
			Msg("saved new keyshare from keyrefresh")

	case dkls.SessionTypeSign:
		// Log signature
		// TODO: Save signature to database for outbound Tx Processing
		sm.logger.Info().
			Str("event_id", eventID).
			Int("signature_len", len(result.Signature)).
			Msg("signature generated from sign session")

	default:
		return errors.Errorf("unknown session type: %s", sessionType)
	}

	// Update event status to SUCCESS (common for all session types)
	if err := sm.eventStore.UpdateStatus(eventID, eventstore.StatusSuccess, ""); err != nil {
		return errors.Wrapf(err, "failed to update event status")
	}

	sm.logger.Info().Str("event_id", eventID).Msg("session finished successfully")

	return nil
}

// createSession creates a new DKLS session based on event type.
func (sm *SessionManager) createSession(ctx context.Context, event *store.TSSEvent, msg *coordinator.Message) (dkls.Session, error) {
	threshold := coordinator.CalculateThreshold(len(msg.Participants))

	switch event.ProtocolType {
	case "keygen":
		return dkls.NewKeygenSession(
			msg.Payload, // setupData
			msg.EventID, // sessionID
			sm.partyID,
			msg.Participants,
			threshold,
		)

	case "keyrefresh":
		// Get current keyID
		keyID, err := sm.coordinator.GetCurrentTSSKeyId(ctx)
		if err != nil {
			return nil, errors.Wrap(err, "failed to get current TSS keyId")
		}

		// Load old keyshare
		oldKeyshare, err := sm.keyshareManager.Get(keyID)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to load keyshare for keyId %s", keyID)
		}

		return dkls.NewKeyrefreshSession(
			msg.Payload, // setupData
			msg.EventID, // sessionID
			sm.partyID,
			msg.Participants,
			threshold,
			oldKeyshare,
		)

	case "sign":
		// Get current keyID
		keyID, err := sm.coordinator.GetCurrentTSSKeyId(ctx)
		if err != nil {
			return nil, errors.Wrap(err, "failed to get current TSS keyId")
		}

		// Load keyshare
		keyshareBytes, err := sm.keyshareManager.Get(keyID)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to load keyshare for keyId %s", keyID)
		}

		// Extract message hash from event data
		messageHash, err := extractMessageHash(event.EventData)
		if err != nil {
			return nil, errors.Wrap(err, "failed to extract message hash")
		}

		return dkls.NewSignSession(
			msg.Payload, // setupData
			msg.EventID, // sessionID
			sm.partyID,
			msg.Participants,
			keyshareBytes,
			messageHash,
			nil, // chainPath
		)

	default:
		return nil, errors.Errorf("unknown protocol type: %s", event.ProtocolType)
	}
}

// validateParticipants validates that participants match protocol requirements.
// For keygen/keyrefresh: participants must match exactly with eligible participants (same elements).
// For sign: participants must be a valid >2/3 subset of eligible participants.
func (sm *SessionManager) validateParticipants(participants []string, event *store.TSSEvent) error {
	// Get eligible validators for this protocol
	eligible := sm.coordinator.GetEligibleUV(string(event.ProtocolType))
	if len(eligible) == 0 {
		return errors.New("no eligible validators for protocol")
	}

	// Build set and list of eligible partyIDs
	eligibleSet := make(map[string]bool)
	eligibleList := make([]string, 0, len(eligible))
	for _, v := range eligible {
		eligibleSet[v.ValidatorAddress] = true
		eligibleList = append(eligibleList, v.ValidatorAddress)
	}

	// Validate all participants are eligible
	participantSet := make(map[string]bool)
	for _, partyID := range participants {
		if !eligibleSet[partyID] {
			return errors.Errorf("participant %s is not eligible for protocol %s", partyID, event.ProtocolType)
		}
		participantSet[partyID] = true
	}

	// Protocol-specific validation
	switch event.ProtocolType {
	case "keygen", "keyrefresh":
		// For keygen and keyrefresh: participants must match exactly with eligible participants
		if len(participants) != len(eligibleList) {
			return errors.Errorf("participants count %d does not match eligible count %d for %s", len(participants), len(eligibleList), event.ProtocolType)
		}
		// Check all eligible are in participants
		for _, eligibleID := range eligibleList {
			if !participantSet[eligibleID] {
				return errors.Errorf("eligible participant %s is missing from participants list for %s", eligibleID, event.ProtocolType)
			}
		}

	case "sign":
		// For sign: participants must be exactly equal to threshold (no more, no less)
		threshold := coordinator.CalculateThreshold(len(eligibleList))
		if len(participants) != threshold {
			return errors.Errorf("participants count %d must equal threshold %d (required >2/3 of %d eligible) for sign", len(participants), threshold, len(eligibleList))
		}
		// All participants must be from eligible set (already validated above)

	default:
		return errors.Errorf("unknown protocol type: %s", event.ProtocolType)
	}

	return nil
}

// extractMessageHash extracts the message hash from event data.
func extractMessageHash(eventData []byte) ([]byte, error) {
	// Try to parse as JSON first
	var eventDataJSON map[string]interface{}
	if err := json.Unmarshal(eventData, &eventDataJSON); err == nil {
		// Successfully parsed as JSON, try to get "message" field
		if msg, ok := eventDataJSON["message"].(string); ok {
			// Hash the message
			hash := sha256.Sum256([]byte(msg))
			return hash[:], nil
		}
		return nil, errors.New("event data JSON does not contain 'message' string field")
	}

	// Not JSON, treat eventData as the message string directly
	if len(eventData) == 0 {
		return nil, errors.New("message is empty")
	}

	// Hash the message
	hash := sha256.Sum256(eventData)
	return hash[:], nil
}
