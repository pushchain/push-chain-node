package reverthandler

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/pushchain/push-chain-node/universalClient/store"
	"github.com/pushchain/push-chain-node/universalClient/tss/coordinator"
	"github.com/pushchain/push-chain-node/universalClient/tss/eventstore"
)

// --- Test helpers ---

func setupTestDB(t *testing.T) *gorm.DB {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}
	if err := db.AutoMigrate(&store.Event{}); err != nil {
		t.Fatalf("failed to migrate database: %v", err)
	}
	return db
}

func setupTestHandler(t *testing.T) (*Handler, *eventstore.Store) {
	db := setupTestDB(t)
	logger := zerolog.Nop()
	evtStore := eventstore.NewStore(db, logger)
	handler := NewHandler(Config{
		EventStore:    evtStore,
		CheckInterval: 1 * time.Second,
		Logger:        logger,
	})
	return handler, evtStore
}

func createEvent(t *testing.T, db *gorm.DB, eventID string, eventType string, status string, blockHeight, expiryHeight uint64, eventData []byte) {
	if eventData == nil {
		eventData, _ = json.Marshal(map[string]any{"key_id": "test"})
	}
	event := store.Event{
		EventID:           eventID,
		Type:              eventType,
		Status:            status,
		BlockHeight:       blockHeight,
		ExpiryBlockHeight: expiryHeight,
		EventData:         eventData,
	}
	if err := db.Create(&event).Error; err != nil {
		t.Fatalf("failed to create event: %v", err)
	}
}

func makeSignEventData(txID, utxID string) []byte {
	data, _ := json.Marshal(map[string]any{
		"tx_id":  txID,
		"utx_id": utxID,
	})
	return data
}

// --- Pure helper tests ---

func TestParseCAIPTxHash(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		chainID   string
		txHash    string
		expectErr bool
	}{
		{
			name:    "EVM chain",
			input:   "eip155:11155111:0xabc123",
			chainID: "eip155:11155111",
			txHash:  "0xabc123",
		},
		{
			name:    "Solana chain",
			input:   "solana:5eykt4UsFv8P8NJdTREpY1vzqKqZKvdp:3AsuLkFgEF",
			chainID: "solana:5eykt4UsFv8P8NJdTREpY1vzqKqZKvdp",
			txHash:  "3AsuLkFgEF",
		},
		{
			name:      "empty string",
			input:     "",
			expectErr: true,
		},
		{
			name:      "no colon",
			input:     "nocolonhere",
			expectErr: true,
		},
		{
			name:      "colon at start",
			input:     ":0xabc",
			expectErr: true,
		},
		{
			name:      "colon at end",
			input:     "eip155:11155111:",
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chainID, txHash, err := parseCAIPTxHash(tt.input)
			if tt.expectErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if chainID != tt.chainID {
				t.Errorf("chainID = %q, want %q", chainID, tt.chainID)
			}
			if txHash != tt.txHash {
				t.Errorf("txHash = %q, want %q", txHash, tt.txHash)
			}
		})
	}
}

func TestErrorMsgForStatus(t *testing.T) {
	tests := []struct {
		status string
		want   string
	}{
		{eventstore.StatusConfirmed, "event expired before TSS started"},
		{eventstore.StatusBroadcasted, "broadcast attempted but tx not found on chain"},
		{"UNKNOWN", "event expired or failed"},
	}

	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			got := errorMsgForStatus(tt.status)
			if got != tt.want {
				t.Errorf("errorMsgForStatus(%s) = %q, want %q", tt.status, got, tt.want)
			}
		})
	}
}

func TestExtractOutboundIDs(t *testing.T) {
	t.Run("valid outbound event data", func(t *testing.T) {
		event := &store.Event{
			EventID:   "event-1",
			EventData: makeSignEventData("tx-123", "utx-456"),
		}
		txID, utxID, err := extractOutboundIDs(event)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if txID != "tx-123" {
			t.Errorf("txID = %q, want %q", txID, "tx-123")
		}
		if utxID != "utx-456" {
			t.Errorf("utxID = %q, want %q", utxID, "utx-456")
		}
	})

	t.Run("missing tx_id", func(t *testing.T) {
		data, _ := json.Marshal(map[string]any{"utx_id": "utx-456"})
		event := &store.Event{EventID: "event-1", EventData: data}
		_, _, err := extractOutboundIDs(event)
		if err == nil {
			t.Fatal("expected error for missing tx_id")
		}
	})

	t.Run("invalid JSON", func(t *testing.T) {
		event := &store.Event{EventID: "event-1", EventData: []byte("not-json")}
		_, _, err := extractOutboundIDs(event)
		if err == nil {
			t.Fatal("expected error for invalid JSON")
		}
	})
}

// --- Handler logic tests (using real DB) ---

func TestHandleEvent_KeyEvents(t *testing.T) {
	keyTypes := []string{
		string(coordinator.ProtocolKeygen),
		string(coordinator.ProtocolKeyrefresh),
		string(coordinator.ProtocolQuorumChange),
	}

	for _, eventType := range keyTypes {
		t.Run(eventType+" marks reverted", func(t *testing.T) {
			h, _ := setupTestHandler(t)
			db := setupTestDB(t)
			evtStore := eventstore.NewStore(db, zerolog.Nop())
			h.eventStore = evtStore

			createEvent(t, db, "key-1", eventType, eventstore.StatusBroadcasted, 100, 200, nil)

			event, _ := evtStore.GetEvent("key-1")
			h.handleEvent(context.Background(), event)

			updated, _ := evtStore.GetEvent("key-1")
			if updated.Status != eventstore.StatusReverted {
				t.Errorf("status = %s, want %s", updated.Status, eventstore.StatusReverted)
			}
		})
	}
}

func TestHandleEvent_UnknownType(t *testing.T) {
	h, _ := setupTestHandler(t)
	db := setupTestDB(t)
	evtStore := eventstore.NewStore(db, zerolog.Nop())
	h.eventStore = evtStore

	createEvent(t, db, "unknown-1", "UNKNOWN_TYPE", eventstore.StatusBroadcasted, 100, 200, nil)

	event, _ := evtStore.GetEvent("unknown-1")
	h.handleEvent(context.Background(), event)

	// Status should remain unchanged (no revert for unknown types)
	updated, _ := evtStore.GetEvent("unknown-1")
	if updated.Status != eventstore.StatusBroadcasted {
		t.Errorf("status = %s, want %s (unchanged)", updated.Status, eventstore.StatusBroadcasted)
	}
}

func TestRevertSignEvent_NoPushSigner(t *testing.T) {
	h, _ := setupTestHandler(t)
	db := setupTestDB(t)
	evtStore := eventstore.NewStore(db, zerolog.Nop())
	h.eventStore = evtStore

	createEvent(t, db, "sign-1", string(coordinator.ProtocolSign), eventstore.StatusBroadcasted, 100, 200, makeSignEventData("tx-1", "utx-1"))

	event, _ := evtStore.GetEvent("sign-1")
	err := h.revertSignEvent(context.Background(), event)

	// Should fail because pushSigner is nil
	if err == nil {
		t.Fatal("expected error when pushSigner is nil")
	}
	if err.Error() != "pushSigner not configured — cannot vote to revert" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRevertSignEvent_InvalidEventData(t *testing.T) {
	h, _ := setupTestHandler(t)
	db := setupTestDB(t)
	evtStore := eventstore.NewStore(db, zerolog.Nop())
	h.eventStore = evtStore

	createEvent(t, db, "sign-bad", string(coordinator.ProtocolSign), eventstore.StatusBroadcasted, 100, 200, []byte("invalid-json"))

	event, _ := evtStore.GetEvent("sign-bad")
	err := h.revertSignEvent(context.Background(), event)

	if err == nil {
		t.Fatal("expected error for invalid event data")
	}
}

func TestVerifyTxOnChain_NilChains(t *testing.T) {
	h, _ := setupTestHandler(t)
	// h.chains is nil by default from setupTestHandler

	found, confs, status, err := h.verifyTxOnChain(context.Background(), "event-1", "eip155:11155111", "0xabc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found {
		t.Error("expected found=false when chains is nil")
	}
	if confs != 0 || status != 0 {
		t.Errorf("expected zero values, got confs=%d status=%d", confs, status)
	}
}

func TestNewHandler_DefaultInterval(t *testing.T) {
	logger := zerolog.Nop()
	h := NewHandler(Config{
		Logger: logger,
	})
	if h.checkInterval != 30*time.Second {
		t.Errorf("default checkInterval = %v, want 30s", h.checkInterval)
	}
}

func TestNewHandler_CustomInterval(t *testing.T) {
	logger := zerolog.Nop()
	h := NewHandler(Config{
		CheckInterval: 5 * time.Second,
		Logger:        logger,
	})
	if h.checkInterval != 5*time.Second {
		t.Errorf("checkInterval = %v, want 5s", h.checkInterval)
	}
}

func TestVerifyAndRevertBroadcasted_InvalidCAIPHash(t *testing.T) {
	h, _ := setupTestHandler(t)
	// pushSigner is nil, so voteFailure will fail — but we can check it reaches that path
	event := &store.Event{
		EventID:           "event-1",
		Status:            eventstore.StatusBroadcasted,
		BroadcastedTxHash: "nocolon",
		EventData:         makeSignEventData("tx-1", "utx-1"),
	}

	err := h.verifyAndRevertBroadcasted(context.Background(), event, "tx-1", "utx-1")
	// Should try to voteFailure which fails because pushSigner is nil
	if err == nil {
		t.Fatal("expected error (pushSigner nil)")
	}
}

func TestStart_CancelsCleanly(t *testing.T) {
	h, _ := setupTestHandler(t)
	h.checkInterval = 10 * time.Second // Long interval so it won't tick

	ctx, cancel := context.WithCancel(context.Background())
	h.Start(ctx)

	// Cancel immediately — goroutine should exit without panic
	cancel()
	time.Sleep(50 * time.Millisecond)
}
