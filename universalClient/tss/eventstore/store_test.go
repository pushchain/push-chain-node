package eventstore

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/pushchain/push-chain-node/universalClient/chains/common"
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
	createTestEventWithType(t, s, eventID, blockHeight, status, expiryHeight, common.EventTypeKeygen)
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
		createTestEvent(t, s, "event-1", 95, StatusConfirmed, 200)

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
		createTestEvent(t, s, "event-1", 80, StatusConfirmed, 200)
		createTestEvent(t, s, "event-2", 85, StatusConfirmed, 200)

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
		createTestEvent(t, s, "pending-1", 80, StatusConfirmed, 200)
		createTestEvent(t, s, "in-progress-1", 80, StatusInProgress, 200)
		createTestEvent(t, s, "success-1", 80, StatusCompleted, 200)
		createTestEvent(t, s, "reverted-1", 80, StatusReverted, 200)

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
		createTestEvent(t, s, "expired-1", 80, StatusConfirmed, 90) // expired (expiry 90 < current 100)
		createTestEvent(t, s, "valid-1", 80, StatusConfirmed, 200)  // not expired (expiry 200 > current 100)
		createTestEvent(t, s, "valid-2", 80, StatusConfirmed, 101)  // not expired (expiry 101 > current 100)

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
		createTestEvent(t, s, "event-1", 80, StatusConfirmed, 200)
		createTestEvent(t, s, "event-2", 85, StatusConfirmed, 200)
		createTestEvent(t, s, "event-3", 88, StatusConfirmed, 200)

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
		createTestEvent(t, s, "event-1", 80, StatusConfirmed, 200)
		time.Sleep(10 * time.Millisecond) // Ensure different created_at
		createTestEvent(t, s, "event-2", 80, StatusConfirmed, 200)
		time.Sleep(10 * time.Millisecond)
		createTestEvent(t, s, "event-3", 75, StatusConfirmed, 200) // Earlier block

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
		createTestEvent(t, s, "event-1", 0, StatusConfirmed, 200)

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
		createTestEvent(t, s, "event-1", 100, StatusConfirmed, 200)

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
		if event.Status != StatusConfirmed {
			t.Errorf("GetEvent() status = %s, want %s", event.Status, StatusConfirmed)
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
		createTestEvent(t, s, "event-1", 100, StatusConfirmed, 200)

		err := s.Update("event-1", map[string]any{"status": StatusInProgress})
		if err != nil {
			t.Fatalf("Update() error = %v, want nil", err)
		}

		event, err := s.GetEvent("event-1")
		if err != nil {
			t.Fatalf("GetEvent() error = %v, want nil", err)
		}
		if event.Status != StatusInProgress {
			t.Errorf("Update() status = %s, want %s", event.Status, StatusInProgress)
		}
	})

	t.Run("update multiple fields", func(t *testing.T) {
		s := setupTestStore(t)
		createTestEvent(t, s, "event-1", 100, StatusInProgress, 200)

		err := s.Update("event-1", map[string]any{
			"status":       StatusConfirmed,
			"block_height": uint64(150),
		})
		if err != nil {
			t.Fatalf("Update() error = %v, want nil", err)
		}

		event, _ := s.GetEvent("event-1")
		if event.Status != StatusConfirmed {
			t.Errorf("status = %s, want %s", event.Status, StatusConfirmed)
		}
		if event.BlockHeight != 150 {
			t.Errorf("block_height = %d, want 150", event.BlockHeight)
		}
	})

	t.Run("update broadcasted tx hash", func(t *testing.T) {
		s := setupTestStore(t)
		createTestEvent(t, s, "event-1", 100, StatusBroadcasted, 200)

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

		err := s.Update("non-existent", map[string]any{"status": StatusCompleted})
		if err == nil {
			t.Fatal("Update() error = nil, want error")
		}
	})

	t.Run("multiple sequential updates", func(t *testing.T) {
		s := setupTestStore(t)
		createTestEvent(t, s, "event-1", 100, StatusConfirmed, 200)

		// CONFIRMED -> IN_PROGRESS
		if err := s.Update("event-1", map[string]any{"status": StatusInProgress}); err != nil {
			t.Fatalf("Update() error = %v", err)
		}
		event, _ := s.GetEvent("event-1")
		if event.Status != StatusInProgress {
			t.Errorf("status = %s, want %s", event.Status, StatusInProgress)
		}

		// IN_PROGRESS -> COMPLETED
		if err := s.Update("event-1", map[string]any{"status": StatusCompleted}); err != nil {
			t.Fatalf("Update() error = %v", err)
		}
		event, _ = s.GetEvent("event-1")
		if event.Status != StatusCompleted {
			t.Errorf("status = %s, want %s", event.Status, StatusCompleted)
		}
	})
}

func TestCountInProgress(t *testing.T) {
	t.Run("no in-progress events", func(t *testing.T) {
		s := setupTestStore(t)
		createTestEvent(t, s, "event-1", 100, StatusConfirmed, 200)
		createTestEvent(t, s, "event-2", 100, StatusCompleted, 200)

		count, err := s.CountInProgress()
		if err != nil {
			t.Fatalf("CountInProgress() error = %v, want nil", err)
		}
		if count != 0 {
			t.Errorf("CountInProgress() = %d, want 0", count)
		}
	})

	t.Run("some in-progress events", func(t *testing.T) {
		s := setupTestStore(t)
		createTestEvent(t, s, "event-1", 100, StatusInProgress, 200)
		createTestEvent(t, s, "event-2", 100, StatusInProgress, 200)
		createTestEvent(t, s, "event-3", 100, StatusConfirmed, 200)

		count, err := s.CountInProgress()
		if err != nil {
			t.Fatalf("CountInProgress() error = %v, want nil", err)
		}
		if count != 2 {
			t.Errorf("CountInProgress() = %d, want 2", count)
		}
	})
}

func TestResetInProgressEventsToConfirmed(t *testing.T) {
	t.Run("resets in-progress events", func(t *testing.T) {
		s := setupTestStore(t)
		createTestEvent(t, s, "ip-1", 100, StatusInProgress, 200)
		createTestEvent(t, s, "ip-2", 100, StatusInProgress, 200)
		createTestEvent(t, s, "confirmed-1", 100, StatusConfirmed, 200)

		count, err := s.ResetInProgressEventsToConfirmed()
		if err != nil {
			t.Fatalf("ResetInProgressEventsToConfirmed() error = %v, want nil", err)
		}
		if count != 2 {
			t.Errorf("ResetInProgressEventsToConfirmed() reset %d, want 2", count)
		}

		// Verify all are now CONFIRMED
		for _, id := range []string{"ip-1", "ip-2", "confirmed-1"} {
			event, _ := s.GetEvent(id)
			if event.Status != StatusConfirmed {
				t.Errorf("event %s status = %s, want %s", id, event.Status, StatusConfirmed)
			}
		}
	})

	t.Run("no in-progress events", func(t *testing.T) {
		s := setupTestStore(t)
		createTestEvent(t, s, "event-1", 100, StatusConfirmed, 200)

		count, err := s.ResetInProgressEventsToConfirmed()
		if err != nil {
			t.Fatalf("ResetInProgressEventsToConfirmed() error = %v, want nil", err)
		}
		if count != 0 {
			t.Errorf("ResetInProgressEventsToConfirmed() reset %d, want 0", count)
		}
	})

	t.Run("does not affect other statuses", func(t *testing.T) {
		s := setupTestStore(t)
		createTestEvent(t, s, "reverted-1", 100, StatusReverted, 200)
		createTestEvent(t, s, "broadcasted-1", 100, StatusBroadcasted, 200)
		createTestEvent(t, s, "ip-1", 100, StatusInProgress, 200)

		count, _ := s.ResetInProgressEventsToConfirmed()
		if count != 1 {
			t.Errorf("ResetInProgressEventsToConfirmed() reset %d, want 1", count)
		}

		// REVERTED and BROADCASTED should be unchanged
		reverted, _ := s.GetEvent("reverted-1")
		if reverted.Status != StatusReverted {
			t.Errorf("reverted event status = %s, want %s", reverted.Status, StatusReverted)
		}
		broadcasted, _ := s.GetEvent("broadcasted-1")
		if broadcasted.Status != StatusBroadcasted {
			t.Errorf("broadcasted event status = %s, want %s", broadcasted.Status, StatusBroadcasted)
		}
	})
}

func TestGetExpiredConfirmedEvents(t *testing.T) {
	t.Run("returns only expired CONFIRMED events", func(t *testing.T) {
		s := setupTestStore(t)
		// Expired CONFIRMED (should be returned)
		createTestEvent(t, s, "confirmed-expired", 50, StatusConfirmed, 90)
		// Expired non-CONFIRMED (should NOT be returned)
		createTestEvent(t, s, "ip-expired", 50, StatusInProgress, 95)
		createTestEvent(t, s, "signed-expired", 50, StatusSigned, 95)
		createTestEvent(t, s, "broadcasted-expired", 50, StatusBroadcasted, 100)
		// Not expired
		createTestEvent(t, s, "confirmed-valid", 50, StatusConfirmed, 200)
		// Terminal statuses (should not be returned)
		createTestEvent(t, s, "completed", 50, StatusCompleted, 90)
		createTestEvent(t, s, "reverted", 50, StatusReverted, 90)

		events, err := s.GetExpiredConfirmedEvents(100, 100)
		if err != nil {
			t.Fatalf("GetExpiredConfirmedEvents() error = %v, want nil", err)
		}
		if len(events) != 1 {
			t.Errorf("GetExpiredConfirmedEvents() returned %d events, want 1", len(events))
		}
		if len(events) > 0 && events[0].EventID != "confirmed-expired" {
			t.Errorf("GetExpiredConfirmedEvents() event ID = %s, want confirmed-expired", events[0].EventID)
		}
	})

	t.Run("no expired events", func(t *testing.T) {
		s := setupTestStore(t)
		createTestEvent(t, s, "event-1", 50, StatusConfirmed, 200)

		events, err := s.GetExpiredConfirmedEvents(100, 100)
		if err != nil {
			t.Fatalf("GetExpiredConfirmedEvents() error = %v, want nil", err)
		}
		if len(events) != 0 {
			t.Errorf("GetExpiredConfirmedEvents() returned %d events, want 0", len(events))
		}
	})

	t.Run("respects limit", func(t *testing.T) {
		s := setupTestStore(t)
		createTestEvent(t, s, "expired-1", 50, StatusConfirmed, 90)
		createTestEvent(t, s, "expired-2", 60, StatusConfirmed, 95)
		createTestEvent(t, s, "expired-3", 70, StatusConfirmed, 99)

		events, err := s.GetExpiredConfirmedEvents(100, 2)
		if err != nil {
			t.Fatalf("GetExpiredConfirmedEvents() error = %v, want nil", err)
		}
		if len(events) != 2 {
			t.Errorf("GetExpiredConfirmedEvents() returned %d events, want 2", len(events))
		}
	})

	t.Run("orders by block height", func(t *testing.T) {
		s := setupTestStore(t)
		createTestEvent(t, s, "expired-high", 70, StatusConfirmed, 90)
		createTestEvent(t, s, "expired-low", 50, StatusConfirmed, 90)

		events, err := s.GetExpiredConfirmedEvents(100, 100)
		if err != nil {
			t.Fatalf("GetExpiredConfirmedEvents() error = %v, want nil", err)
		}
		if events[0].EventID != "expired-low" {
			t.Errorf("first event = %s, want expired-low", events[0].EventID)
		}
	})
}
