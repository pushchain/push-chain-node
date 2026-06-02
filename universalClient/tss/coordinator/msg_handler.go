package coordinator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"

	"github.com/pushchain/push-chain-node/universalClient/store"
	utsstypes "github.com/pushchain/push-chain-node/x/utss/types"
)

// Sentinel errors validateIncomingRequest may return for ACKs that are
// well-formed but should be silently ignored. Callers map both to a nil
// return; the messages exist for clarity in debug logs and stacks.
var (
	errEventNotTracked = errors.New("event is not tracked by this coordinator")
	errDuplicateACK    = errors.New("duplicate ACK from sender")
)

// isSkippableACKError reports whether err is one of the silent-skip sentinels
// from validateIncomingRequest.
func isSkippableACKError(err error) bool {
	return errors.Is(err, errEventNotTracked) || errors.Is(err, errDuplicateACK)
}

// HandleIncomingMessage routes a coordinator-bound message. Caller unmarshals.
func (c *Coordinator) HandleIncomingMessage(ctx context.Context, peerID string, msg *Message) error {
	c.logger.Debug().
		Str("peer_id", peerID).
		Str("type", string(msg.Type)).
		Str("event_id", msg.EventID).
		Msg("coordinator handling incoming message")

	switch msg.Type {
	case MessageTypeACK:
		if msg.SignedData != nil {
			return c.handleSignedAck(ctx, peerID, msg.EventID, msg.SignedData)
		}
		return c.handleUnsignedAck(ctx, peerID, msg.EventID)
	default:
		return fmt.Errorf("unknown coordinator message type: %s", msg.Type)
	}
}

// validateIncomingRequest checks that the coordinator is tracking the event,
// the sender is a listed participant, and the sender hasn't already ACKed.
// Returns nil if the ACK should be processed, errSkipACK if it should be
// silently ignored (untracked or duplicate — logged at debug), or a
// descriptive error when the sender is reachable but not a participant.
// Snapshots state under ackMu.RLock so the caller doesn't hold the lock
// across GetPartyIDFromPeerID.
func (c *Coordinator) validateIncomingRequest(ctx context.Context, eventID, senderPeerID string) error {
	c.ackMu.RLock()
	state, ok := c.ackTracking[eventID]
	var participants []string
	var alreadyAcked bool
	if ok {
		participants = append([]string(nil), state.participants...)
		alreadyAcked = state.ackedBy[senderPeerID]
	}
	c.ackMu.RUnlock()

	if participants == nil {
		return fmt.Errorf("event %s: %w", eventID, errEventNotTracked)
	}

	senderPartyID, err := c.GetPartyIDFromPeerID(ctx, senderPeerID)
	if err != nil {
		return fmt.Errorf("failed to get partyID for sender peerID %s: %w", senderPeerID, err)
	}
	isParticipant := false
	for _, p := range participants {
		if p == senderPartyID {
			isParticipant = true
			break
		}
	}
	if !isParticipant {
		return fmt.Errorf("sender %s (partyID: %s) is not a participant for event %s", senderPeerID, senderPartyID, eventID)
	}
	if alreadyAcked {
		c.logger.Debug().
			Str("event_id", eventID).
			Str("sender", senderPeerID).
			Msg("duplicate ACK received, ignoring")
		return fmt.Errorf("sender %s on event %s: %w", senderPeerID, eventID, errDuplicateACK)
	}
	return nil
}

// handleUnsignedAck counts a plain ACK and, when all participants have ACKed,
// sends BEGIN and clears ackTracking. No signature is involved.
func (c *Coordinator) handleUnsignedAck(ctx context.Context, senderPeerID string, eventID string) error {
	if err := c.validateIncomingRequest(ctx, eventID, senderPeerID); err != nil {
		if isSkippableACKError(err) {
			return nil
		}
		return err
	}

	c.ackMu.Lock()
	defer c.ackMu.Unlock()

	// Race guard: between the helper's RLock and this Lock another goroutine
	// could have cleared tracking or ACKed for this peer. Re-check silently.
	state, exists := c.ackTracking[eventID]
	if !exists || state.ackedBy[senderPeerID] {
		return nil
	}

	state.ackedBy[senderPeerID] = true
	state.ackCount++

	c.logger.Debug().
		Str("event_id", eventID).
		Str("sender", senderPeerID).
		Int("ack_count", state.ackCount).
		Int("expected_participants", len(state.participants)).
		Msg("coordinator received ACK")

	if state.ackCount == len(state.participants) {
		c.logger.Info().
			Str("event_id", eventID).
			Int("total_participants", len(state.participants)).
			Msg("all participants ACKed, coordinator will send BEGIN message")

		// Send BEGIN message to all participants
		beginMsg := Message{
			Type:         MessageTypeBegin,
			EventID:      eventID,
			Payload:      nil,
			Participants: state.participants,
		}
		beginMsgBytes, err := json.Marshal(beginMsg)
		if err != nil {
			return fmt.Errorf("failed to marshal begin message: %w", err)
		}

		// Send to all participants
		for _, participantPartyID := range state.participants {
			participantPeerID, err := c.GetPeerIDFromPartyID(ctx, participantPartyID)
			if err != nil {
				c.logger.Warn().
					Err(err).
					Str("participant_party_id", participantPartyID).
					Msg("failed to get peerID for participant, skipping begin message")
				continue
			}

			if err := c.send(ctx, participantPeerID, beginMsgBytes); err != nil {
				c.logger.Warn().
					Err(err).
					Str("participant_peer_id", participantPeerID).
					Str("participant_party_id", participantPartyID).
					Msg("failed to send begin message to participant")
				continue
			}

			c.logger.Debug().
				Str("event_id", eventID).
				Str("participant_peer_id", participantPeerID).
				Msg("coordinator sent begin message to participant")
		}

		// Clean up ACK tracking after sending BEGIN
		delete(c.ackTracking, eventID)
	}

	return nil
}

// handleSignedAck verifies a prior signature claimed in an ACK and, on
// success, drops ackTracking so no BEGIN goes out. Hash is rebuilt from
// event data — the peer's announced hash is not trusted. Fund-migration
// events are verified against the OLD TSS pubkey (signer of the migration tx).
// Sender must be a tracked participant; untracked events are silently ignored.
func (c *Coordinator) handleSignedAck(ctx context.Context, senderPeerID, eventID string, signedData *SignedDataPayload) error {
	if len(signedData.Signature) != 64 && len(signedData.Signature) != 65 {
		return fmt.Errorf("signature must be 64 or 65 bytes, got %d", len(signedData.Signature))
	}

	if err := c.validateIncomingRequest(ctx, eventID, senderPeerID); err != nil {
		if isSkippableACKError(err) {
			return nil
		}
		return err
	}

	event, err := c.eventStore.GetEvent(eventID)
	if err != nil {
		return fmt.Errorf("event %s not found in store: %w", eventID, err)
	}
	expectedHash, err := c.rebuildSigningHash(ctx, event, signedData.Nonce, signedData.TSSFundMigrationAmount)
	if err != nil {
		return fmt.Errorf("rebuild signing hash for event %s: %w", eventID, err)
	}
	if !bytes.Equal(expectedHash, signedData.SigningHash) {
		return fmt.Errorf("announced signing_hash does not match rebuilt hash for event %s", eventID)
	}
	pubkeyHex, err := c.verifyingPubkey(ctx, event)
	if err != nil {
		return fmt.Errorf("resolve verifying pubkey for event %s: %w", eventID, err)
	}
	if err := verifyECDSASignature(pubkeyHex, expectedHash, signedData.Signature); err != nil {
		return fmt.Errorf("signature verification failed for event %s from %s: %w", eventID, senderPeerID, err)
	}

	c.ackMu.Lock()
	delete(c.ackTracking, eventID)
	c.ackMu.Unlock()

	c.logger.Debug().
		Str("event_id", eventID).
		Str("sender", senderPeerID).
		Msg("ACK carried verified prior signature; cancelling coordination for this event")

	return nil
}

// verifyingPubkey returns the compressed pubkey hex that should have signed
// the event: current TSS key for SIGN_OUTBOUND, OldTssPubkey for fund
// migration (signed by the old TSS to sweep funds to the new TSS).
func (c *Coordinator) verifyingPubkey(ctx context.Context, event *store.Event) (string, error) {
	switch event.Type {
	case store.EventTypeSignOutbound:
		_, hex, err := c.GetCurrentTSSKey(ctx)
		if err != nil {
			return "", fmt.Errorf("fetch current TSS key: %w", err)
		}
		if hex == "" {
			return "", fmt.Errorf("no current TSS key configured")
		}
		return hex, nil
	case store.EventTypeSignFundMigrate:
		var migrationData utsstypes.FundMigrationInitiatedEventData
		if err := json.Unmarshal(event.EventData, &migrationData); err != nil {
			return "", fmt.Errorf("unmarshal fund migration event data: %w", err)
		}
		if migrationData.OldTssPubkey == "" {
			return "", fmt.Errorf("fund migration event missing old_tss_pubkey")
		}
		return migrationData.OldTssPubkey, nil
	default:
		return "", fmt.Errorf("event type %s has no verifying pubkey", event.Type)
	}
}

func (c *Coordinator) rebuildSigningHash(ctx context.Context, event *store.Event, nonce uint64, claimedAmount *big.Int) ([]byte, error) {
	switch event.Type {
	case store.EventTypeSignOutbound:
		req, err := c.buildSignTransaction(ctx, event.EventData, &nonce)
		if err != nil {
			return nil, err
		}
		return req.SigningHash, nil
	case store.EventTypeSignFundMigrate:
		if claimedAmount == nil || claimedAmount.Sign() <= 0 {
			return nil, fmt.Errorf("fund migration verification requires positive claimed amount")
		}
		req, err := c.buildFundMigrationTransaction(ctx, event.EventData, &nonce, claimedAmount)
		if err != nil {
			return nil, err
		}
		return req.SigningHash, nil
	default:
		return nil, fmt.Errorf("event type %s has no signature to verify", event.Type)
	}
}
