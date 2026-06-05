package eventstore

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/pushchain/push-chain-node/universalClient/store"
)

// setupTestDB creates an in-memory SQLite database for testing.
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

// setupTestStore creates a test event store with an in-memory database.
func setupTestStore(t *testing.T) *Store {
	db := setupTestDB(t)
	logger := zerolog.Nop()
	return NewStore(db, logger)
}

// createTestEvent creates a test TSS event in the database.
func createTestEvent(t *testing.T, s *Store, eventID string, blockHeight uint64, status string, expiryHeight uint64) {
	createTestEventWithType(t, s, eventID, blockHeight, status, expiryHeight, store.EventTypeKeygen)
}

// createTestEventWithType creates a test event with a specific type.
func createTestEventWithType(t *testing.T, s *Store, eventID string, blockHeight uint64, status string, expiryHeight uint64, eventType string) {
	eventData, _ := json.Marshal(map[string]any{
		"key_id": "test-key-1",
	})

	event := store.Event{
		EventID:           eventID,
		BlockHeight:       blockHeight,
		ExpiryBlockHeight: expiryHeight,
		Type:              eventType,
		Status:            status,
		EventData:         eventData,
	}

	if err := s.db.Create(&event).Error; err != nil {
		t.Fatalf("failed to create test event: %v", err)
	}
}

func TestNewStore(t *testing.T) {
	db := setupTestDB(t)
	logger := zerolog.Nop()

	store := NewStore(db, logger)
	if store == nil {
		t.Fatal("NewStore() returned nil")
	}
	if store.db == nil {
		t.Fatal("NewStore() returned store with nil db")
	}
}

func TestGetNonExpiredConfirmedEvents(t *testing.T) {
	t.Run("no events", func(t *testing.T) {
		s := setupTestStore(t)
		events, err := s.GetNonExpiredConfirmedEvents(100, 10, 0)
		if err != nil {
			t.Fatalf("GetNonExpiredConfirmedEvents() error = %v, want nil", err)
		}
		if len(events) != 0 {
			t.Errorf("GetNonExpiredConfirmedEvents() returned %d events, want 0", len(events))
		}
	})

	t.Run("events not ready (too recent)", func(t *testing.T) {
		s := setupTestStore(t)
		// Create event at block 95, current block is 100, min confirmation is 10
		// Event is only 5 blocks old, needs 10 blocks confirmation
		createTestEvent(t, s, "event-1", 95, store.StatusConfirmed, 200)

		events, err := s.GetNonExpiredConfirmedEvents(100, 10, 0)
		if err != nil {
			t.Fatalf("GetNonExpiredConfirmedEvents() error = %v, want nil", err)
		}
		if len(events) != 0 {
			t.Errorf("GetNonExpiredConfirmedEvents() returned %d events, want 0 (event too recent)", len(events))
		}
	})

	t.Run("events ready (old enough)", func(t *testing.T) {
		s := setupTestStore(t)
		// Create event at block 80, current block is 100, min confirmation is 10
		// Event is 20 blocks old, should be ready
		createTestEvent(t, s, "event-1", 80, store.StatusConfirmed, 200)
		createTestEvent(t, s, "event-2", 85, store.StatusConfirmed, 200)

		events, err := s.GetNonExpiredConfirmedEvents(100, 10, 0)
		if err != nil {
			t.Fatalf("GetNonExpiredConfirmedEvents() error = %v, want nil", err)
		}
		if len(events) != 2 {
			t.Errorf("GetNonExpiredConfirmedEvents() returned %d events, want 2", len(events))
		}
		if events[0].EventID != "event-1" {
			t.Errorf("GetNonExpiredConfirmedEvents() first event ID = %s, want event-1", events[0].EventID)
		}
		if events[1].EventID != "event-2" {
			t.Errorf("GetNonExpiredConfirmedEvents() second event ID = %s, want event-2", events[1].EventID)
		}
	})

	t.Run("filters non-pending events", func(t *testing.T) {
		s := setupTestStore(t)
		createTestEvent(t, s, "pending-1", 80, store.StatusConfirmed, 200)
		createTestEvent(t, s, "in-progress-1", 80, store.StatusInProgress, 200)
		createTestEvent(t, s, "success-1", 80, store.StatusCompleted, 200)
		createTestEvent(t, s, "reverted-1", 80, store.StatusReverted, 200)

		events, err := s.GetNonExpiredConfirmedEvents(100, 10, 0)
		if err != nil {
			t.Fatalf("GetNonExpiredConfirmedEvents() error = %v, want nil", err)
		}
		if len(events) != 1 {
			t.Errorf("GetNonExpiredConfirmedEvents() returned %d events, want 1", len(events))
		}
		if events[0].EventID != "pending-1" {
			t.Errorf("GetNonExpiredConfirmedEvents() event ID = %s, want pending-1", events[0].EventID)
		}
	})

	t.Run("excludes expired events", func(t *testing.T) {
		s := setupTestStore(t)
		createTestEvent(t, s, "expired-1", 80, store.StatusConfirmed, 90) // expired (expiry 90 < current 100)
		createTestEvent(t, s, "valid-1", 80, store.StatusConfirmed, 200)  // not expired (expiry 200 > current 100)
		createTestEvent(t, s, "valid-2", 80, store.StatusConfirmed, 101)  // not expired (expiry 101 > current 100)

		events, err := s.GetNonExpiredConfirmedEvents(100, 10, 0)
		if err != nil {
			t.Fatalf("GetNonExpiredConfirmedEvents() error = %v, want nil", err)
		}
		if len(events) != 2 {
			t.Errorf("GetNonExpiredConfirmedEvents() returned %d events, want 2", len(events))
		}
	})

	t.Run("respects limit", func(t *testing.T) {
		s := setupTestStore(t)
		createTestEvent(t, s, "event-1", 80, store.StatusConfirmed, 200)
		createTestEvent(t, s, "event-2", 85, store.StatusConfirmed, 200)
		createTestEvent(t, s, "event-3", 88, store.StatusConfirmed, 200)

		events, err := s.GetNonExpiredConfirmedEvents(100, 10, 2)
		if err != nil {
			t.Fatalf("GetNonExpiredConfirmedEvents() error = %v, want nil", err)
		}
		if len(events) != 2 {
			t.Errorf("GetNonExpiredConfirmedEvents() returned %d events, want 2", len(events))
		}
	})

	t.Run("orders by block number and created_at", func(t *testing.T) {
		s := setupTestStore(t)
		createTestEvent(t, s, "event-1", 80, store.StatusConfirmed, 200)
		time.Sleep(10 * time.Millisecond) // Ensure different created_at
		createTestEvent(t, s, "event-2", 80, store.StatusConfirmed, 200)
		time.Sleep(10 * time.Millisecond)
		createTestEvent(t, s, "event-3", 75, store.StatusConfirmed, 200) // Earlier block

		events, err := s.GetNonExpiredConfirmedEvents(100, 10, 0)
		if err != nil {
			t.Fatalf("GetNonExpiredConfirmedEvents() error = %v, want nil", err)
		}
		if len(events) != 3 {
			t.Fatalf("GetNonExpiredConfirmedEvents() returned %d events, want 3", len(events))
		}
		// Should be ordered: event-3 (block 75), event-1 (block 80), event-2 (block 80)
		if events[0].EventID != "event-3" {
			t.Errorf("first event ID = %s, want event-3", events[0].EventID)
		}
		if events[1].EventID != "event-1" {
			t.Errorf("second event ID = %s, want event-1", events[1].EventID)
		}
		if events[2].EventID != "event-2" {
			t.Errorf("third event ID = %s, want event-2", events[2].EventID)
		}
	})

	t.Run("handles current block less than min confirmation", func(t *testing.T) {
		s := setupTestStore(t)
		createTestEvent(t, s, "event-1", 0, store.StatusConfirmed, 200)

		// Current block is 5, min confirmation is 10
		events, err := s.GetNonExpiredConfirmedEvents(5, 10, 0)
		if err != nil {
			t.Fatalf("GetNonExpiredConfirmedEvents() error = %v, want nil", err)
		}
		if len(events) != 1 {
			t.Errorf("GetNonExpiredConfirmedEvents() returned %d events, want 1", len(events))
		}
	})
}

func TestGetEvent(t *testing.T) {
	t.Run("event exists", func(t *testing.T) {
		s := setupTestStore(t)
		createTestEvent(t, s, "event-1", 100, store.StatusConfirmed, 200)

		event, err := s.GetEvent("event-1")
		if err != nil {
			t.Fatalf("GetEvent() error = %v, want nil", err)
		}
		if event == nil {
			t.Fatal("GetEvent() returned nil event")
		}
		if event.EventID != "event-1" {
			t.Errorf("GetEvent() event ID = %s, want event-1", event.EventID)
		}
		if event.BlockHeight != 100 {
			t.Errorf("GetEvent() block height = %d, want 100", event.BlockHeight)
		}
		if event.Status != store.StatusConfirmed {
			t.Errorf("GetEvent() status = %s, want %s", event.Status, store.StatusConfirmed)
		}
	})

	t.Run("event does not exist", func(t *testing.T) {
		s := setupTestStore(t)

		event, err := s.GetEvent("non-existent")
		if err == nil {
			t.Fatal("GetEvent() error = nil, want error")
		}
		if event != nil {
			t.Fatal("GetEvent() returned non-nil event on error")
		}
	})
}

func TestUpdate(t *testing.T) {
	t.Run("update status", func(t *testing.T) {
		s := setupTestStore(t)
		createTestEvent(t, s, "event-1", 100, store.StatusConfirmed, 200)

		err := s.Update("event-1", map[string]any{"status": store.StatusInProgress})
		if err != nil {
			t.Fatalf("Update() error = %v, want nil", err)
		}

		event, err := s.GetEvent("event-1")
		if err != nil {
			t.Fatalf("GetEvent() error = %v, want nil", err)
		}
		if event.Status != store.StatusInProgress {
			t.Errorf("Update() status = %s, want %s", event.Status, store.StatusInProgress)
		}
	})

	t.Run("update multiple fields", func(t *testing.T) {
		s := setupTestStore(t)
		createTestEvent(t, s, "event-1", 100, store.StatusInProgress, 200)

		err := s.Update("event-1", map[string]any{
			"status":       store.StatusConfirmed,
			"block_height": uint64(150),
		})
		if err != nil {
			t.Fatalf("Update() error = %v, want nil", err)
		}

		event, _ := s.GetEvent("event-1")
		if event.Status != store.StatusConfirmed {
			t.Errorf("status = %s, want %s", event.Status, store.StatusConfirmed)
		}
		if event.BlockHeight != 150 {
			t.Errorf("block_height = %d, want 150", event.BlockHeight)
		}
	})

	t.Run("update broadcasted tx hash", func(t *testing.T) {
		s := setupTestStore(t)
		createTestEvent(t, s, "event-1", 100, store.StatusBroadcasted, 200)

		err := s.Update("event-1", map[string]any{"broadcasted_tx_hash": "eip155:11155111:0xabc"})
		if err != nil {
			t.Fatalf("Update() error = %v, want nil", err)
		}

		event, _ := s.GetEvent("event-1")
		if event.BroadcastedTxHash != "eip155:11155111:0xabc" {
			t.Errorf("broadcasted_tx_hash = %s, want eip155:11155111:0xabc", event.BroadcastedTxHash)
		}
	})

	t.Run("update non-existent event", func(t *testing.T) {
		s := setupTestStore(t)

		err := s.Update("non-existent", map[string]any{"status": store.StatusCompleted})
		if err == nil {
			t.Fatal("Update() error = nil, want error")
		}
	})

	t.Run("multiple sequential updates", func(t *testing.T) {
		s := setupTestStore(t)
		createTestEvent(t, s, "event-1", 100, store.StatusConfirmed, 200)

		// CONFIRMED -> IN_PROGRESS
		if err := s.Update("event-1", map[string]any{"status": store.StatusInProgress}); err != nil {
			t.Fatalf("Update() error = %v", err)
		}
		event, _ := s.GetEvent("event-1")
		if event.Status != store.StatusInProgress {
			t.Errorf("status = %s, want %s", event.Status, store.StatusInProgress)
		}

		// IN_PROGRESS -> COMPLETED
		if err := s.Update("event-1", map[string]any{"status": store.StatusCompleted}); err != nil {
			t.Fatalf("Update() error = %v", err)
		}
		event, _ = s.GetEvent("event-1")
		if event.Status != store.StatusCompleted {
			t.Errorf("status = %s, want %s", event.Status, store.StatusCompleted)
		}
	})
}

func TestHasSigningData(t *testing.T) {
	validSig := strings.Repeat("ab", 64)   // 64 bytes (r||s)
	validSig65 := strings.Repeat("cd", 65) // 65 bytes (r||s||v)
	validHash := strings.Repeat("ef", 32)  // 32 bytes

	cases := []struct {
		name string
		body string
		want bool
	}{
		{"empty bytes", "", false},
		{"not json", "not json", false},
		{"no signing_data key", `{"foo":"bar"}`, false},
		{"signing_data is null", `{"signing_data":null}`, false},
		{"signing_data missing signature", fmt.Sprintf(
			`{"signing_data":{"signing_hash":"%s"}}`, validHash), false},
		{"signing_data missing signing_hash", fmt.Sprintf(
			`{"signing_data":{"signature":"%s"}}`, validSig), false},
		{"signature not hex", fmt.Sprintf(
			`{"signing_data":{"signature":"zzz","signing_hash":"%s"}}`, validHash), false},
		{"signing_hash not hex", fmt.Sprintf(
			`{"signing_data":{"signature":"%s","signing_hash":"zzz"}}`, validSig), false},
		{"signature wrong length (32B)", fmt.Sprintf(
			`{"signing_data":{"signature":"%s","signing_hash":"%s"}}`,
			strings.Repeat("ab", 32), validHash), false},
		{"signing_hash wrong length (16B)", fmt.Sprintf(
			`{"signing_data":{"signature":"%s","signing_hash":"%s"}}`,
			validSig, strings.Repeat("ef", 16)), false},
		{"valid 64-byte signature", fmt.Sprintf(
			`{"signing_data":{"signature":"%s","signing_hash":"%s","nonce":42}}`,
			validSig, validHash), true},
		{"valid 65-byte signature (with v)", fmt.Sprintf(
			`{"signing_data":{"signature":"%s","signing_hash":"%s","nonce":42}}`,
			validSig65, validHash), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := hasSigningData([]byte(tc.body)); got != tc.want {
				t.Errorf("hasSigningData(%q) = %v, want %v", tc.body, got, tc.want)
			}
		})
	}
}

func TestRecoverInProgressEvents(t *testing.T) {
	// Helper: seed an IN_PROGRESS event whose event_data carries signing_data
	// (mimicking a row whose status was clobbered after PersistSignature ran).
	// 64-byte signature (r||s, 128 hex chars), 32-byte hash (64 hex chars).
	validSig := strings.Repeat("ab", 64)
	validHash := strings.Repeat("cd", 32)
	createInProgressWithSigningData := func(t *testing.T, s *Store, id string) {
		t.Helper()
		body := []byte(fmt.Sprintf(
			`{"foo":"bar","signing_data":{"signature":"%s","signing_hash":"%s","nonce":1}}`,
			validSig, validHash))
		ev := store.Event{
			EventID:     id,
			BlockHeight: 100,
			Type:        store.EventTypeSignOutbound,
			Status:      store.StatusInProgress,
			EventData:   body,
		}
		if err := s.db.Create(&ev).Error; err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}

	t.Run("rescues IN_PROGRESS with signing_data to SIGNED, resets the rest to CONFIRMED", func(t *testing.T) {
		s := setupTestStore(t)
		createInProgressWithSigningData(t, s, "rescue-1")
		createInProgressWithSigningData(t, s, "rescue-2")
		createTestEvent(t, s, "plain-ip-1", 100, store.StatusInProgress, 200) // no signing_data
		createTestEvent(t, s, "confirmed-1", 100, store.StatusConfirmed, 200)

		signedRecovered, confirmedReset, err := s.RecoverInProgressEvents()
		if err != nil {
			t.Fatalf("RecoverInProgressEvents: %v", err)
		}
		if signedRecovered != 2 {
			t.Errorf("signedRecovered = %d, want 2", signedRecovered)
		}
		if confirmedReset != 1 {
			t.Errorf("confirmedReset = %d, want 1", confirmedReset)
		}

		for _, id := range []string{"rescue-1", "rescue-2"} {
			ev, _ := s.GetEvent(id)
			if ev.Status != store.StatusSigned {
				t.Errorf("%s status = %s, want SIGNED", id, ev.Status)
			}
		}
		ev, _ := s.GetEvent("plain-ip-1")
		if ev.Status != store.StatusConfirmed {
			t.Errorf("plain-ip-1 status = %s, want CONFIRMED", ev.Status)
		}
		ev, _ = s.GetEvent("confirmed-1")
		if ev.Status != store.StatusConfirmed {
			t.Errorf("confirmed-1 should be untouched, status = %s", ev.Status)
		}
	})

	t.Run("no IN_PROGRESS events", func(t *testing.T) {
		s := setupTestStore(t)
		createTestEvent(t, s, "event-1", 100, store.StatusConfirmed, 200)

		signedRecovered, confirmedReset, err := s.RecoverInProgressEvents()
		if err != nil {
			t.Fatalf("RecoverInProgressEvents: %v", err)
		}
		if signedRecovered != 0 || confirmedReset != 0 {
			t.Errorf("counts = (%d, %d), want (0, 0)", signedRecovered, confirmedReset)
		}
	})

	t.Run("does not affect other statuses", func(t *testing.T) {
		s := setupTestStore(t)
		createTestEvent(t, s, "reverted-1", 100, store.StatusReverted, 200)
		createTestEvent(t, s, "broadcasted-1", 100, store.StatusBroadcasted, 200)
		createTestEvent(t, s, "ip-1", 100, store.StatusInProgress, 200)

		_, confirmedReset, _ := s.RecoverInProgressEvents()
		if confirmedReset != 1 {
			t.Errorf("confirmedReset = %d, want 1", confirmedReset)
		}

		reverted, _ := s.GetEvent("reverted-1")
		if reverted.Status != store.StatusReverted {
			t.Errorf("reverted status = %s", reverted.Status)
		}
		broadcasted, _ := s.GetEvent("broadcasted-1")
		if broadcasted.Status != store.StatusBroadcasted {
			t.Errorf("broadcasted status = %s", broadcasted.Status)
		}
	})

	t.Run("malformed signing_data treated as no signing_data", func(t *testing.T) {
		// Stricter than just "bad JSON": structurally valid JSON with a
		// signing_data block whose fields fail length/hex validation must NOT
		// be promoted to SIGNED — the broadcaster would fail to assemble.
		cases := []struct {
			id   string
			body []byte
		}{
			{"non-json", []byte(`not json at all`)},
			{"sig-wrong-length", []byte(
				`{"signing_data":{"signature":"ab","signing_hash":"` +
					strings.Repeat("ef", 32) + `"}}`)},
			{"sig-not-hex", []byte(
				`{"signing_data":{"signature":"zzzz","signing_hash":"` +
					strings.Repeat("ef", 32) + `"}}`)},
		}
		s := setupTestStore(t)
		for _, c := range cases {
			if err := s.db.Create(&store.Event{
				EventID:     c.id,
				BlockHeight: 100,
				Type:        store.EventTypeSignOutbound,
				Status:      store.StatusInProgress,
				EventData:   c.body,
			}).Error; err != nil {
				t.Fatalf("seed %s: %v", c.id, err)
			}
		}
		signedRecovered, confirmedReset, err := s.RecoverInProgressEvents()
		if err != nil {
			t.Fatalf("RecoverInProgressEvents: %v", err)
		}
		if signedRecovered != 0 || confirmedReset != int64(len(cases)) {
			t.Errorf("counts = (%d, %d), want (0, %d)", signedRecovered, confirmedReset, len(cases))
		}
		for _, c := range cases {
			ev, _ := s.GetEvent(c.id)
			if ev.Status != store.StatusConfirmed {
				t.Errorf("%s status = %s, want CONFIRMED", c.id, ev.Status)
			}
		}
	})
}

func TestGetInFlightSignEvents(t *testing.T) {
	s := setupTestStore(t)

	createTestEventWithType(t, s, "sign-inprogress", 10, store.StatusInProgress, 200, store.EventTypeSignOutbound)
	createTestEventWithType(t, s, "sign-signed", 11, store.StatusSigned, 200, store.EventTypeSignOutbound)
	createTestEventWithType(t, s, "sign-broadcasted", 12, store.StatusBroadcasted, 200, store.EventTypeSignOutbound)
	createTestEventWithType(t, s, "keygen-inprogress", 13, store.StatusInProgress, 200, store.EventTypeKeygen)

	events, err := s.GetInFlightSignEvents()
	if err != nil {
		t.Fatalf("GetInFlightSignEvents() error = %v", err)
	}
	// Should return IN_PROGRESS + SIGNED sign events, NOT broadcasted, NOT keygen
	if len(events) != 2 {
		t.Fatalf("GetInFlightSignEvents() returned %d events, want 2", len(events))
	}
}

func TestGetInFlightSignEvents_IncludesFundMigrate(t *testing.T) {
	s := setupTestStore(t)

	createTestEventWithType(t, s, "sign-inprogress", 10, store.StatusInProgress, 200, store.EventTypeSignOutbound)
	createTestEventWithType(t, s, "fm-inprogress", 11, store.StatusInProgress, 200, store.EventTypeSignFundMigrate)
	createTestEventWithType(t, s, "fm-signed", 12, store.StatusSigned, 200, store.EventTypeSignFundMigrate)
	createTestEventWithType(t, s, "keygen-inprogress", 13, store.StatusInProgress, 200, store.EventTypeKeygen)

	events, err := s.GetInFlightSignEvents()
	if err != nil {
		t.Fatalf("GetInFlightSignEvents() error = %v", err)
	}
	// Should return both SIGN_OUTBOUND and SIGN_FUND_MIGRATE, NOT keygen
	if len(events) != 3 {
		t.Fatalf("GetInFlightSignEvents() returned %d events, want 3", len(events))
	}
}

func TestGetSignedSignEvents(t *testing.T) {
	s := setupTestStore(t)

	createTestEventWithType(t, s, "signed-1", 10, store.StatusSigned, 200, store.EventTypeSignOutbound)
	createTestEventWithType(t, s, "signed-2", 11, store.StatusSigned, 200, store.EventTypeSignOutbound)
	createTestEventWithType(t, s, "inprogress-1", 12, store.StatusInProgress, 200, store.EventTypeSignOutbound)

	t.Run("returns only signed events", func(t *testing.T) {
		events, err := s.GetSignedSignEvents(10)
		if err != nil {
			t.Fatalf("GetSignedSignEvents() error = %v", err)
		}
		if len(events) != 2 {
			t.Errorf("GetSignedSignEvents() returned %d events, want 2", len(events))
		}
	})

	t.Run("respects limit", func(t *testing.T) {
		events, err := s.GetSignedSignEvents(1)
		if err != nil {
			t.Fatalf("GetSignedSignEvents() error = %v", err)
		}
		if len(events) != 1 {
			t.Errorf("GetSignedSignEvents(1) returned %d events, want 1", len(events))
		}
	})

	t.Run("zero limit defaults to 50", func(t *testing.T) {
		events, err := s.GetSignedSignEvents(0)
		if err != nil {
			t.Fatalf("GetSignedSignEvents(0) error = %v", err)
		}
		if len(events) != 2 {
			t.Errorf("GetSignedSignEvents(0) returned %d events, want 2", len(events))
		}
	})
}

func TestGetSignedSignEvents_IncludesFundMigrate(t *testing.T) {
	s := setupTestStore(t)

	createTestEventWithType(t, s, "signed-outbound", 10, store.StatusSigned, 200, store.EventTypeSignOutbound)
	createTestEventWithType(t, s, "signed-fm", 11, store.StatusSigned, 200, store.EventTypeSignFundMigrate)

	events, err := s.GetSignedSignEvents(10)
	if err != nil {
		t.Fatalf("GetSignedSignEvents() error = %v", err)
	}
	if len(events) != 2 {
		t.Errorf("GetSignedSignEvents() returned %d events, want 2", len(events))
	}
}

func TestGetBroadcastedSignEvents(t *testing.T) {
	s := setupTestStore(t)

	// Create broadcasted event with tx hash
	evt := store.Event{
		EventID:           "bc-1",
		BlockHeight:       10,
		ExpiryBlockHeight: 200,
		Type:              store.EventTypeSignOutbound,
		Status:            store.StatusBroadcasted,
		BroadcastedTxHash: "0xhash123",
	}
	if err := s.db.Create(&evt).Error; err != nil {
		t.Fatalf("failed to create event: %v", err)
	}

	// Create broadcasted event without tx hash — should be excluded
	evt2 := store.Event{
		EventID:           "bc-2",
		BlockHeight:       11,
		ExpiryBlockHeight: 200,
		Type:              store.EventTypeSignOutbound,
		Status:            store.StatusBroadcasted,
		BroadcastedTxHash: "",
	}
	if err := s.db.Create(&evt2).Error; err != nil {
		t.Fatalf("failed to create event: %v", err)
	}

	t.Run("returns only events with tx hash", func(t *testing.T) {
		events, err := s.GetBroadcastedSignEvents(10)
		if err != nil {
			t.Fatalf("GetBroadcastedSignEvents() error = %v", err)
		}
		if len(events) != 1 {
			t.Errorf("got %d events, want 1", len(events))
		}
		if events[0].EventID != "bc-1" {
			t.Errorf("got event %s, want bc-1", events[0].EventID)
		}
	})

	t.Run("zero limit defaults to 50", func(t *testing.T) {
		events, err := s.GetBroadcastedSignEvents(0)
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if len(events) != 1 {
			t.Errorf("got %d events, want 1", len(events))
		}
	})
}

func TestGetBroadcastedSignEvents_IncludesFundMigrate(t *testing.T) {
	s := setupTestStore(t)

	evt := store.Event{
		EventID:           "bc-outbound",
		BlockHeight:       10,
		ExpiryBlockHeight: 200,
		Type:              store.EventTypeSignOutbound,
		Status:            store.StatusBroadcasted,
		BroadcastedTxHash: "0xhash1",
	}
	if err := s.db.Create(&evt).Error; err != nil {
		t.Fatalf("failed to create event: %v", err)
	}

	evt2 := store.Event{
		EventID:           "bc-fm",
		BlockHeight:       11,
		ExpiryBlockHeight: 200,
		Type:              store.EventTypeSignFundMigrate,
		Status:            store.StatusBroadcasted,
		BroadcastedTxHash: "0xhash2",
	}
	if err := s.db.Create(&evt2).Error; err != nil {
		t.Fatalf("failed to create event: %v", err)
	}

	events, err := s.GetBroadcastedSignEvents(10)
	if err != nil {
		t.Fatalf("GetBroadcastedSignEvents() error = %v", err)
	}
	if len(events) != 2 {
		t.Errorf("got %d events, want 2", len(events))
	}
}

// ---------------------------------------------------------------------------
// PersistSignature
// ---------------------------------------------------------------------------

func TestPersistSignature(t *testing.T) {
	baseEventData := []byte(`{"destination_chain":"eip155:1","recipient":"0xabc"}`)
	sig := []byte{0xbe, 0xef}
	hash := []byte{0xde, 0xad}

	t.Run("from CONFIRMED flips to SIGNED and merges signing_data", func(t *testing.T) {
		s := setupTestStore(t)
		event := store.Event{
			EventID:   "ev-1",
			Type:      store.EventTypeSignOutbound,
			Status:    store.StatusConfirmed,
			EventData: baseEventData,
		}
		if err := s.db.Create(&event).Error; err != nil {
			t.Fatalf("seed event: %v", err)
		}

		persisted, err := s.PersistSignature("ev-1", baseEventData, sig, hash, 42, nil)
		if err != nil {
			t.Fatalf("PersistSignature: %v", err)
		}
		if !persisted {
			t.Fatal("expected persisted=true, got false")
		}

		got, err := s.GetEvent("ev-1")
		if err != nil {
			t.Fatalf("GetEvent: %v", err)
		}
		if got.Status != store.StatusSigned {
			t.Errorf("status = %q, want SIGNED", got.Status)
		}
		var raw map[string]any
		if err := json.Unmarshal(got.EventData, &raw); err != nil {
			t.Fatalf("unmarshal event_data: %v", err)
		}
		sd, ok := raw["signing_data"].(map[string]any)
		if !ok {
			t.Fatalf("signing_data missing or wrong type: %T", raw["signing_data"])
		}
		if sd["signature"] != "beef" {
			t.Errorf("signature = %v, want beef", sd["signature"])
		}
		if sd["signing_hash"] != "dead" {
			t.Errorf("signing_hash = %v, want dead", sd["signing_hash"])
		}
	})

	t.Run("from IN_PROGRESS also flips to SIGNED", func(t *testing.T) {
		s := setupTestStore(t)
		event := store.Event{
			EventID:   "ev-2",
			Type:      store.EventTypeSignOutbound,
			Status:    store.StatusInProgress,
			EventData: baseEventData,
		}
		if err := s.db.Create(&event).Error; err != nil {
			t.Fatalf("seed event: %v", err)
		}

		persisted, err := s.PersistSignature("ev-2", baseEventData, sig, hash, 7, nil)
		if err != nil {
			t.Fatalf("PersistSignature: %v", err)
		}
		if !persisted {
			t.Fatal("expected persisted=true")
		}
		got, _ := s.GetEvent("ev-2")
		if got.Status != store.StatusSigned {
			t.Errorf("status = %q, want SIGNED", got.Status)
		}
	})

	t.Run("skips when already SIGNED", func(t *testing.T) {
		s := setupTestStore(t)
		event := store.Event{
			EventID:   "ev-3",
			Type:      store.EventTypeSignOutbound,
			Status:    store.StatusSigned,
			EventData: baseEventData,
		}
		if err := s.db.Create(&event).Error; err != nil {
			t.Fatalf("seed event: %v", err)
		}

		persisted, err := s.PersistSignature("ev-3", baseEventData, sig, hash, 1, nil)
		if err != nil {
			t.Fatalf("PersistSignature: %v", err)
		}
		if persisted {
			t.Fatal("expected persisted=false (status guard)")
		}
		// event_data left untouched
		got, _ := s.GetEvent("ev-3")
		if string(got.EventData) != string(baseEventData) {
			t.Errorf("event_data mutated when it should have been left alone")
		}
	})

	t.Run("skips when BROADCASTED (no clobber)", func(t *testing.T) {
		// Critical: late writer must not undo a successful BROADCASTED transition.
		s := setupTestStore(t)
		event := store.Event{
			EventID:           "ev-4",
			Type:              store.EventTypeSignOutbound,
			Status:            store.StatusBroadcasted,
			EventData:         baseEventData,
			BroadcastedTxHash: "eip155:1:0xdeadbeef",
		}
		if err := s.db.Create(&event).Error; err != nil {
			t.Fatalf("seed event: %v", err)
		}

		persisted, err := s.PersistSignature("ev-4", baseEventData, sig, hash, 1, nil)
		if err != nil {
			t.Fatalf("PersistSignature: %v", err)
		}
		if persisted {
			t.Fatal("expected persisted=false (BROADCASTED guard)")
		}
		got, _ := s.GetEvent("ev-4")
		if got.Status != store.StatusBroadcasted {
			t.Errorf("status = %q, want BROADCASTED (clobbered!)", got.Status)
		}
		if got.BroadcastedTxHash != "eip155:1:0xdeadbeef" {
			t.Errorf("broadcasted_tx_hash mutated")
		}
	})

	t.Run("invalid event data JSON returns error", func(t *testing.T) {
		s := setupTestStore(t)
		_, err := s.PersistSignature("ev-5", []byte("not json"), sig, hash, 1, nil)
		if err == nil {
			t.Fatal("expected error on invalid JSON")
		}
	})
}
