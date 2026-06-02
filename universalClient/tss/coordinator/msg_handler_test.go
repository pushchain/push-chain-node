package coordinator

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/universalClient/store"
	utsstypes "github.com/pushchain/push-chain-node/x/utss/types"
)

func TestHandleIncomingMessage(t *testing.T) {
	coord, _, _ := setupTestCoordinator(t)
	ctx := context.Background()

	t.Run("unknown type rejected", func(t *testing.T) {
		err := coord.HandleIncomingMessage(ctx, "peer1", &Message{Type: "garbage", EventID: "e1"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unknown coordinator message type")
	})

	t.Run("ACK without SignedData routes to handleUnsignedAck", func(t *testing.T) {
		// Untracked event → handleUnsignedAck returns nil without error.
		err := coord.HandleIncomingMessage(ctx, "peer1", &Message{Type: MessageTypeACK, EventID: "untracked"})
		assert.NoError(t, err)
	})
}

func TestHandleUnsignedAck(t *testing.T) {
	coord, _, _ := setupTestCoordinator(t)
	ctx := context.Background()

	t.Run("ack for untracked event is ignored", func(t *testing.T) {
		err := coord.handleUnsignedAck(ctx, "peer1", "unknown-event")
		assert.NoError(t, err)
	})

	t.Run("ack tracking with registered event", func(t *testing.T) {
		coord.ackMu.Lock()
		coord.ackTracking["test-event"] = &ackState{
			participants: []string{"validator1", "validator2", "validator3"},
			ackedBy:      make(map[string]bool),
			ackCount:     0,
		}
		coord.ackMu.Unlock()

		// First ACK
		err := coord.handleUnsignedAck(ctx, "peer1", "test-event")
		assert.NoError(t, err)

		coord.ackMu.RLock()
		state := coord.ackTracking["test-event"]
		assert.Equal(t, 1, state.ackCount)
		assert.True(t, state.ackedBy["peer1"])
		coord.ackMu.RUnlock()

		// Duplicate ACK from same peer should not increment
		err = coord.handleUnsignedAck(ctx, "peer1", "test-event")
		assert.NoError(t, err)

		coord.ackMu.RLock()
		assert.Equal(t, 1, coord.ackTracking["test-event"].ackCount)
		coord.ackMu.RUnlock()

		// ACK from second peer
		err = coord.handleUnsignedAck(ctx, "peer2", "test-event")
		assert.NoError(t, err)

		coord.ackMu.RLock()
		assert.Equal(t, 2, coord.ackTracking["test-event"].ackCount)
		coord.ackMu.RUnlock()
	})

	t.Run("ack from non-participant is rejected", func(t *testing.T) {
		coord.ackMu.Lock()
		coord.ackTracking["restricted-event"] = &ackState{
			participants: []string{"validator1"},
			ackedBy:      make(map[string]bool),
			ackCount:     0,
		}
		coord.ackMu.Unlock()

		// peer2 maps to validator2 which is not in participants
		err := coord.handleUnsignedAck(ctx, "peer2", "restricted-event")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not a participant")
	})
}

func TestHandleUnsignedAck_UnknownPeerID(t *testing.T) {
	coord, _, _ := setupTestCoordinator(t)
	ctx := context.Background()

	coord.ackMu.Lock()
	coord.ackTracking["evt-unknown-peer"] = &ackState{
		participants: []string{"validator1"},
		ackedBy:      make(map[string]bool),
		ackCount:     0,
	}
	coord.ackMu.Unlock()

	err := coord.handleUnsignedAck(ctx, "totally-unknown-peer", "evt-unknown-peer")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get partyID")
}

func TestHandleUnsignedAck_AllACKsTriggersBEGIN(t *testing.T) {
	coord, _, _ := setupTestCoordinator(t)
	ctx := context.Background()

	var sentMessages []string
	coord.send = func(_ context.Context, peerID string, _ []byte) error {
		sentMessages = append(sentMessages, peerID)
		return nil
	}

	coord.ackMu.Lock()
	coord.ackTracking["evt-begin"] = &ackState{
		participants: []string{"validator1", "validator2"},
		ackedBy:      map[string]bool{"peer1": true},
		ackCount:     1,
	}
	coord.ackMu.Unlock()

	// Second ACK completes the set
	err := coord.handleUnsignedAck(ctx, "peer2", "evt-begin")
	require.NoError(t, err)

	// BEGIN should have been sent to both participants
	assert.Len(t, sentMessages, 2)
	assert.Contains(t, sentMessages, "peer1")
	assert.Contains(t, sentMessages, "peer2")

	// ACK tracking should be cleaned up
	coord.ackMu.RLock()
	_, exists := coord.ackTracking["evt-begin"]
	coord.ackMu.RUnlock()
	assert.False(t, exists, "ack tracking should be removed after all ACKs received")
}

func TestHandleSignedAck_FailurePaths(t *testing.T) {
	coord, _, db := setupTestCoordinator(t)
	ctx := context.Background()

	t.Run("bad signature length rejected", func(t *testing.T) {
		err := coord.handleSignedAck(ctx, "peer1", "evt", &SignedDataPayload{
			Signature:   make([]byte, 10),
			SigningHash: make([]byte, 32),
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "signature must be 64 or 65 bytes")
	})

	t.Run("event not in store rejected", func(t *testing.T) {
		err := coord.handleSignedAck(ctx, "peer1", "no-such-event", &SignedDataPayload{
			Signature:   make([]byte, 64),
			SigningHash: make([]byte, 32),
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found in store")
	})

	t.Run("non-sign event type rejected", func(t *testing.T) {
		require.NoError(t, db.Create(&store.Event{
			EventID:     "keygen-evt",
			BlockHeight: 1,
			Type:        store.EventTypeKeygen,
			Status:      store.StatusConfirmed,
		}).Error)
		err := coord.handleSignedAck(ctx, "peer1", "keygen-evt", &SignedDataPayload{
			Signature:   make([]byte, 64),
			SigningHash: make([]byte, 32),
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "has no signature to verify")
	})

	t.Run("fund migration without claimed amount rejected", func(t *testing.T) {
		require.NoError(t, db.Create(&store.Event{
			EventID:     "fm-evt",
			BlockHeight: 1,
			Type:        store.EventTypeSignFundMigrate,
			Status:      store.StatusConfirmed,
			EventData:   []byte("{}"),
		}).Error)
		err := coord.handleSignedAck(ctx, "peer1", "fm-evt", &SignedDataPayload{
			Signature:   make([]byte, 64),
			SigningHash: make([]byte, 32),
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "requires positive claimed amount")
	})

	t.Run("ack with SignedData does not touch ackTracking on failure", func(t *testing.T) {
		coord.ackMu.Lock()
		coord.ackTracking["tracked-evt"] = &ackState{
			participants: []string{"validator1"},
			ackedBy:      make(map[string]bool),
			ackCount:     0,
		}
		coord.ackMu.Unlock()

		_ = coord.handleSignedAck(ctx, "peer1", "tracked-evt", &SignedDataPayload{
			Signature:   make([]byte, 10),
			SigningHash: make([]byte, 32),
		})

		coord.ackMu.RLock()
		_, stillTracked := coord.ackTracking["tracked-evt"]
		coord.ackMu.RUnlock()
		assert.True(t, stillTracked, "failed verification must not cancel coordination")
	})
}

func TestVerifyingPubkey(t *testing.T) {
	coord, _, _ := setupTestCoordinator(t)
	ctx := context.Background()

	t.Run("FundMigrate returns OldTssPubkey", func(t *testing.T) {
		// This is the bug-fix invariant: fund-migration verification must use
		// the old TSS key (the one that signed the sweep), not the current key.
		data, _ := json.Marshal(utsstypes.FundMigrationInitiatedEventData{
			OldTssPubkey:     "03oldoldoldoldoldoldoldoldoldoldoldoldoldoldoldoldoldoldoldoldoldold",
			CurrentTssPubkey: "03currentcurrentcurrentcurrentcurrentcurrentcurrentcurrentcurrentxxx",
		})
		pub, err := coord.verifyingPubkey(ctx, &store.Event{
			Type:      store.EventTypeSignFundMigrate,
			EventData: data,
		})
		require.NoError(t, err)
		assert.Equal(t, "03oldoldoldoldoldoldoldoldoldoldoldoldoldoldoldoldoldoldoldoldoldold", pub)
	})

	t.Run("FundMigrate with empty OldTssPubkey rejected", func(t *testing.T) {
		data, _ := json.Marshal(utsstypes.FundMigrationInitiatedEventData{OldTssPubkey: ""})
		_, err := coord.verifyingPubkey(ctx, &store.Event{
			Type:      store.EventTypeSignFundMigrate,
			EventData: data,
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "missing old_tss_pubkey")
	})

	t.Run("FundMigrate with bad JSON rejected", func(t *testing.T) {
		_, err := coord.verifyingPubkey(ctx, &store.Event{
			Type:      store.EventTypeSignFundMigrate,
			EventData: []byte("not json"),
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unmarshal")
	})

	t.Run("SignOutbound queries current TSS key", func(t *testing.T) {
		// Test fixture's pushcore is an empty *pushcore.Client → RPC fails.
		// Confirms the SignOutbound branch routes through GetCurrentTSSKey
		// rather than returning a value derived from event data.
		_, err := coord.verifyingPubkey(ctx, &store.Event{Type: store.EventTypeSignOutbound})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "fetch current TSS key")
	})

	t.Run("unknown event type rejected", func(t *testing.T) {
		_, err := coord.verifyingPubkey(ctx, &store.Event{Type: store.EventTypeKeygen})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no verifying pubkey")
	})
}
