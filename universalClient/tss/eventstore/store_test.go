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
	eventData, _ := json.Marshal(map[string]interface{}{
		"key_id": "test-key-1",
	})

	event := store.Event{
		EventID:           eventID,
		BlockHeight:       blockHeight,
		ExpiryBlockHeight: expiryHeight,
		Type:              common.EventTypeKeygen,
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

func TestGetConfirmedEvents(t *testing.T) {
	t.Run("no events", func(t *testing.T) {
		s := setupTestStore(t)
		events, err := s.GetConfirmedEvents(100, 10)
		if err != nil {
			t.Fatalf("GetConfirmedEvents() error = %v, want nil", err)
		}
		if len(events) != 0 {
			t.Errorf("GetConfirmedEvents() returned %d events, want 0", len(events))
		}
	})

	t.Run("events not ready (too recent)", func(t *testing.T) {
		s := setupTestStore(t)
		// Create event at block 95, current block is 100, min confirmation is 10
		// Event is only 5 blocks old, needs 10 blocks confirmation
		createTestEvent(t, s, "event-1", 95, StatusConfirmed, 200)

		events, err := s.GetConfirmedEvents(100, 10)
		if err != nil {
			t.Fatalf("GetConfirmedEvents() error = %v, want nil", err)
		}
		if len(events) != 0 {
			t.Errorf("GetConfirmedEvents() returned %d events, want 0 (event too recent)", len(events))
		}
	})

	t.Run("events ready (old enough)", func(t *testing.T) {
		s := setupTestStore(t)
		// Create event at block 80, current block is 100, min confirmation is 10
		// Event is 20 blocks old, should be ready
		createTestEvent(t, s, "event-1", 80, StatusConfirmed, 200)
		createTestEvent(t, s, "event-2", 85, StatusConfirmed, 200)

		events, err := s.GetConfirmedEvents(100, 10)
		if err != nil {
			t.Fatalf("GetConfirmedEvents() error = %v, want nil", err)
		}
		if len(events) != 2 {
			t.Errorf("GetConfirmedEvents() returned %d events, want 2", len(events))
		}
		if events[0].EventID != "event-1" {
			t.Errorf("GetConfirmedEvents() first event ID = %s, want event-1", events[0].EventID)
		}
		if events[1].EventID != "event-2" {
			t.Errorf("GetConfirmedEvents() second event ID = %s, want event-2", events[1].EventID)
		}
	})

	t.Run("filters non-pending events", func(t *testing.T) {
		s := setupTestStore(t)
		createTestEvent(t, s, "pending-1", 80, StatusConfirmed, 200)
		createTestEvent(t, s, "in-progress-1", 80, StatusInProgress, 200)
		createTestEvent(t, s, "success-1", 80, StatusCompleted, 200)
		createTestEvent(t, s, "reverted-1", 80, StatusReverted, 200)

		events, err := s.GetConfirmedEvents(100, 10)
		if err != nil {
			t.Fatalf("GetConfirmedEvents() error = %v, want nil", err)
		}
		if len(events) != 1 {
			t.Errorf("GetConfirmedEvents() returned %d events, want 1", len(events))
		}
		if events[0].EventID != "pending-1" {
			t.Errorf("GetConfirmedEvents() event ID = %s, want pending-1", events[0].EventID)
		}
	})

	t.Run("excludes expired events", func(t *testing.T) {
		s := setupTestStore(t)
		// Create events with different expiry heights
		createTestEvent(t, s, "expired-1", 80, StatusConfirmed, 90) // expired (expiry 90 < current 100)
		createTestEvent(t, s, "valid-1", 80, StatusConfirmed, 200)  // not expired (expiry 200 > current 100)
		createTestEvent(t, s, "valid-2", 80, StatusConfirmed, 101)  // not expired (expiry 101 > current 100)

		events, err := s.GetConfirmedEvents(100, 10)
		if err != nil {
			t.Fatalf("GetConfirmedEvents() error = %v, want nil", err)
		}
		if len(events) != 2 {
			t.Errorf("GetConfirmedEvents() returned %d events, want 2", len(events))
		}
	})

	t.Run("orders by block number and created_at", func(t *testing.T) {
		s := setupTestStore(t)
		// Create events with same block number but different creation times
		createTestEvent(t, s, "event-1", 80, StatusConfirmed, 200)
		time.Sleep(10 * time.Millisecond) // Ensure different created_at
		createTestEvent(t, s, "event-2", 80, StatusConfirmed, 200)
		time.Sleep(10 * time.Millisecond)
		createTestEvent(t, s, "event-3", 75, StatusConfirmed, 200) // Earlier block

		events, err := s.GetConfirmedEvents(100, 10)
		if err != nil {
			t.Fatalf("GetConfirmedEvents() error = %v, want nil", err)
		}
		if len(events) != 3 {
			t.Fatalf("GetConfirmedEvents() returned %d events, want 3", len(events))
		}
		// Should be ordered: event-3 (block 75), event-1 (block 80), event-2 (block 80)
		if events[0].EventID != "event-3" {
			t.Errorf("GetConfirmedEvents() first event ID = %s, want event-3", events[0].EventID)
		}
		if events[1].EventID != "event-1" {
			t.Errorf("GetConfirmedEvents() second event ID = %s, want event-1", events[1].EventID)
		}
		if events[2].EventID != "event-2" {
			t.Errorf("GetConfirmedEvents() third event ID = %s, want event-2", events[2].EventID)
		}
	})

	t.Run("handles current block less than min confirmation", func(t *testing.T) {
		s := setupTestStore(t)
		createTestEvent(t, s, "event-1", 0, StatusConfirmed, 200)

		// Current block is 5, min confirmation is 10
		events, err := s.GetConfirmedEvents(5, 10)
		if err != nil {
			t.Fatalf("GetConfirmedEvents() error = %v, want nil", err)
		}
		// Should return events at block 0 or earlier
		if len(events) != 1 {
			t.Errorf("GetConfirmedEvents() returned %d events, want 1", len(events))
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

func TestUpdateStatus(t *testing.T) {
	t.Run("update status without error message", func(t *testing.T) {
		s := setupTestStore(t)
		createTestEvent(t, s, "event-1", 100, StatusConfirmed, 200)

		err := s.UpdateStatus("event-1", StatusInProgress, "")
		if err != nil {
			t.Fatalf("UpdateStatus() error = %v, want nil", err)
		}

		event, err := s.GetEvent("event-1")
		if err != nil {
			t.Fatalf("GetEvent() error = %v, want nil", err)
		}
		if event.Status != StatusInProgress {
			t.Errorf("UpdateStatus() status = %s, want %s", event.Status, StatusInProgress)
		}
	})

	t.Run("update status with error message", func(t *testing.T) {
		s := setupTestStore(t)
		createTestEvent(t, s, "event-1", 100, StatusConfirmed, 200)

		errorMsg := "test error message"
		// On failure, events are reset to CONFIRMED for retry
		err := s.UpdateStatus("event-1", StatusConfirmed, errorMsg)
		if err != nil {
			t.Fatalf("UpdateStatus() error = %v, want nil", err)
		}

		event, err := s.GetEvent("event-1")
		if err != nil {
			t.Fatalf("GetEvent() error = %v, want nil", err)
		}
		if event.Status != StatusConfirmed {
			t.Errorf("UpdateStatus() status = %s, want %s", event.Status, StatusConfirmed)
		}
		// Note: ErrorMsg field was removed from store.Event model
	})

	t.Run("update non-existent event", func(t *testing.T) {
		s := setupTestStore(t)

		err := s.UpdateStatus("non-existent", StatusCompleted, "")
		if err == nil {
			t.Fatal("UpdateStatus() error = nil, want error")
		}
	})

	t.Run("multiple status updates", func(t *testing.T) {
		s := setupTestStore(t)
		createTestEvent(t, s, "event-1", 100, StatusConfirmed, 200)

		// CONFIRMED -> IN_PROGRESS
		if err := s.UpdateStatus("event-1", StatusInProgress, ""); err != nil {
			t.Fatalf("UpdateStatus() error = %v", err)
		}
		event, _ := s.GetEvent("event-1")
		if event.Status != StatusInProgress {
			t.Errorf("UpdateStatus() status = %s, want %s", event.Status, StatusInProgress)
		}

		// IN_PROGRESS -> SUCCESS
		if err := s.UpdateStatus("event-1", StatusCompleted, ""); err != nil {
			t.Fatalf("UpdateStatus() error = %v", err)
		}
		event, _ = s.GetEvent("event-1")
		if event.Status != StatusCompleted {
			t.Errorf("UpdateStatus() status = %s, want %s", event.Status, StatusCompleted)
		}
	})
}
