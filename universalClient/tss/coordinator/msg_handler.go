package coordinator

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/big"

	"github.com/pushchain/push-chain-node/universalClient/store"
	utsstypes "github.com/pushchain/push-chain-node/x/utss/types"
)

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

// handleUnsignedAck counts a plain ACK and, when all participants have ACKed,
// sends BEGIN and clears ackTracking. No signature is involved.
func (c *Coordinator) handleUnsignedAck(ctx context.Context, senderPeerID string, eventID string) error {
	c.ackMu.Lock()
	defer c.ackMu.Unlock()

	state, exists := c.ackTracking[eventID]
	if !exists {
		// Not tracking this event, ignore (might be from a different coordinator)
		return nil
	}

	// Check if already ACKed
	if state.ackedBy[senderPeerID] {
		c.logger.Debug().
			Str("event_id", eventID).
			Str("sender", senderPeerID).
			Msg("duplicate ACK received, ignoring")
		return nil
	}

	// Verify sender is a participant
	senderPartyID, err := c.GetPartyIDFromPeerID(ctx, senderPeerID)
	if err != nil {
		return fmt.Errorf("failed to get partyID for sender peerID %s: %w", senderPeerID, err)
	}

	isParticipant := false
	for _, participantPartyID := range state.participants {
		if participantPartyID == senderPartyID {
			isParticipant = true
			break
		}
	}
	if !isParticipant {
		return fmt.Errorf("sender %s (partyID: %s) is not a participant for event %s", senderPeerID, senderPartyID, eventID)
	}

	// Mark as ACKed
	state.ackedBy[senderPeerID] = true
	state.ackCount++

	c.logger.Debug().
		Str("event_id", eventID).
		Str("sender", senderPeerID).
		Str("sender_party_id", senderPartyID).
		Int("ack_count", state.ackCount).
		Int("expected_participants", len(state.participants)).
		Msg("coordinator received ACK")

	// Check if all participants have ACKed
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
func (c *Coordinator) handleSignedAck(ctx context.Context, senderPeerID, eventID string, signedData *SignedDataPayload) error {
	if len(signedData.Signature) != 64 && len(signedData.Signature) != 65 {
		return fmt.Errorf("signature must be 64 or 65 bytes, got %d", len(signedData.Signature))
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
	_, tracked := c.ackTracking[eventID]
	if tracked {
		delete(c.ackTracking, eventID)
	}
	c.ackMu.Unlock()

	c.logger.Debug().
		Str("event_id", eventID).
		Str("sender", senderPeerID).
		Bool("was_tracking", tracked).
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
