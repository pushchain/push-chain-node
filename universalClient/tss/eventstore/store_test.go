package eventstore

import (
	"encoding/json"
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

	if err := db.AutoMigrate(&store.PCEvent{}); err != nil {
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

	event := store.PCEvent{
		EventID:           eventID,
		BlockHeight:       blockHeight,
		ExpiryBlockHeight: expiryHeight,
		Type:              "KEYGEN",
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

func TestGetPendingEvents(t *testing.T) {
	t.Run("no events", func(t *testing.T) {
		s := setupTestStore(t)
		events, err := s.GetPendingEvents(100, 10)
		if err != nil {
			t.Fatalf("GetPendingEvents() error = %v, want nil", err)
		}
		if len(events) != 0 {
			t.Errorf("GetPendingEvents() returned %d events, want 0", len(events))
		}
	})

	t.Run("events not ready (too recent)", func(t *testing.T) {
		s := setupTestStore(t)
		// Create event at block 95, current block is 100, min confirmation is 10
		// Event is only 5 blocks old, needs 10 blocks confirmation
		createTestEvent(t, s, "event-1", 95, StatusPending, 200)

		events, err := s.GetPendingEvents(100, 10)
		if err != nil {
			t.Fatalf("GetPendingEvents() error = %v, want nil", err)
		}
		if len(events) != 0 {
			t.Errorf("GetPendingEvents() returned %d events, want 0 (event too recent)", len(events))
		}
	})

	t.Run("events ready (old enough)", func(t *testing.T) {
		s := setupTestStore(t)
		// Create event at block 80, current block is 100, min confirmation is 10
		// Event is 20 blocks old, should be ready
		createTestEvent(t, s, "event-1", 80, StatusPending, 200)
		createTestEvent(t, s, "event-2", 85, StatusPending, 200)

		events, err := s.GetPendingEvents(100, 10)
		if err != nil {
			t.Fatalf("GetPendingEvents() error = %v, want nil", err)
		}
		if len(events) != 2 {
			t.Errorf("GetPendingEvents() returned %d events, want 2", len(events))
		}
		if events[0].EventID != "event-1" {
			t.Errorf("GetPendingEvents() first event ID = %s, want event-1", events[0].EventID)
		}
		if events[1].EventID != "event-2" {
			t.Errorf("GetPendingEvents() second event ID = %s, want event-2", events[1].EventID)
		}
	})

	t.Run("filters non-pending events", func(t *testing.T) {
		s := setupTestStore(t)
		createTestEvent(t, s, "pending-1", 80, StatusPending, 200)
		createTestEvent(t, s, "in-progress-1", 80, StatusInProgress, 200)
		createTestEvent(t, s, "success-1", 80, StatusSuccess, 200)
		createTestEvent(t, s, "expired-1", 80, StatusExpired, 200)

		events, err := s.GetPendingEvents(100, 10)
		if err != nil {
			t.Fatalf("GetPendingEvents() error = %v, want nil", err)
		}
		if len(events) != 1 {
			t.Errorf("GetPendingEvents() returned %d events, want 1", len(events))
		}
		if events[0].EventID != "pending-1" {
			t.Errorf("GetPendingEvents() event ID = %s, want pending-1", events[0].EventID)
		}
	})

	t.Run("filters expired events", func(t *testing.T) {
		s := setupTestStore(t)
		// Create expired event (expiry at 90, current block is 100)
		createTestEvent(t, s, "expired-1", 80, StatusPending, 90)
		createTestEvent(t, s, "valid-1", 80, StatusPending, 200)

		events, err := s.GetPendingEvents(100, 10)
		if err != nil {
			t.Fatalf("GetPendingEvents() error = %v, want nil", err)
		}
		if len(events) != 1 {
			t.Errorf("GetPendingEvents() returned %d events, want 1", len(events))
		}
		if events[0].EventID != "valid-1" {
			t.Errorf("GetPendingEvents() event ID = %s, want valid-1", events[0].EventID)
		}

		// Verify expired event was marked as expired
		expiredEvent, err := s.GetEvent("expired-1")
		if err != nil {
			t.Fatalf("GetEvent() error = %v, want nil", err)
		}
		if expiredEvent.Status != StatusExpired {
			t.Errorf("expired event status = %s, want %s", expiredEvent.Status, StatusExpired)
		}
	})

	t.Run("orders by block number and created_at", func(t *testing.T) {
		s := setupTestStore(t)
		// Create events with same block number but different creation times
		createTestEvent(t, s, "event-1", 80, StatusPending, 200)
		time.Sleep(10 * time.Millisecond) // Ensure different created_at
		createTestEvent(t, s, "event-2", 80, StatusPending, 200)
		time.Sleep(10 * time.Millisecond)
		createTestEvent(t, s, "event-3", 75, StatusPending, 200) // Earlier block

		events, err := s.GetPendingEvents(100, 10)
		if err != nil {
			t.Fatalf("GetPendingEvents() error = %v, want nil", err)
		}
		if len(events) != 3 {
			t.Fatalf("GetPendingEvents() returned %d events, want 3", len(events))
		}
		// Should be ordered: event-3 (block 75), event-1 (block 80), event-2 (block 80)
		if events[0].EventID != "event-3" {
			t.Errorf("GetPendingEvents() first event ID = %s, want event-3", events[0].EventID)
		}
		if events[1].EventID != "event-1" {
			t.Errorf("GetPendingEvents() second event ID = %s, want event-1", events[1].EventID)
		}
		if events[2].EventID != "event-2" {
			t.Errorf("GetPendingEvents() third event ID = %s, want event-2", events[2].EventID)
		}
	})

	t.Run("handles current block less than min confirmation", func(t *testing.T) {
		s := setupTestStore(t)
		createTestEvent(t, s, "event-1", 0, StatusPending, 200)

		// Current block is 5, min confirmation is 10
		events, err := s.GetPendingEvents(5, 10)
		if err != nil {
			t.Fatalf("GetPendingEvents() error = %v, want nil", err)
		}
		// Should return events at block 0 or earlier
		if len(events) != 1 {
			t.Errorf("GetPendingEvents() returned %d events, want 1", len(events))
		}
	})
}

func TestGetEvent(t *testing.T) {
	t.Run("event exists", func(t *testing.T) {
		s := setupTestStore(t)
		createTestEvent(t, s, "event-1", 100, StatusPending, 200)

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
		if event.Status != StatusPending {
			t.Errorf("GetEvent() status = %s, want %s", event.Status, StatusPending)
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
		createTestEvent(t, s, "event-1", 100, StatusPending, 200)

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
		if event.ErrorMsg != "" {
			t.Errorf("UpdateStatus() error message = %s, want empty", event.ErrorMsg)
		}
	})

	t.Run("update status with error message", func(t *testing.T) {
		s := setupTestStore(t)
		createTestEvent(t, s, "event-1", 100, StatusPending, 200)

		errorMsg := "test error message"
		// On failure, events are reset to PENDING for retry
		err := s.UpdateStatus("event-1", StatusPending, errorMsg)
		if err != nil {
			t.Fatalf("UpdateStatus() error = %v, want nil", err)
		}

		event, err := s.GetEvent("event-1")
		if err != nil {
			t.Fatalf("GetEvent() error = %v, want nil", err)
		}
		if event.Status != StatusPending {
			t.Errorf("UpdateStatus() status = %s, want %s", event.Status, StatusPending)
		}
		if event.ErrorMsg != errorMsg {
			t.Errorf("UpdateStatus() error message = %s, want %s", event.ErrorMsg, errorMsg)
		}
	})

	t.Run("update non-existent event", func(t *testing.T) {
		s := setupTestStore(t)

		err := s.UpdateStatus("non-existent", StatusSuccess, "")
		if err == nil {
			t.Fatal("UpdateStatus() error = nil, want error")
		}
	})

	t.Run("multiple status updates", func(t *testing.T) {
		s := setupTestStore(t)
		createTestEvent(t, s, "event-1", 100, StatusPending, 200)

		// PENDING -> IN_PROGRESS
		if err := s.UpdateStatus("event-1", StatusInProgress, ""); err != nil {
			t.Fatalf("UpdateStatus() error = %v", err)
		}
		event, _ := s.GetEvent("event-1")
		if event.Status != StatusInProgress {
			t.Errorf("UpdateStatus() status = %s, want %s", event.Status, StatusInProgress)
		}

		// IN_PROGRESS -> SUCCESS
		if err := s.UpdateStatus("event-1", StatusSuccess, ""); err != nil {
			t.Fatalf("UpdateStatus() error = %v", err)
		}
		event, _ = s.GetEvent("event-1")
		if event.Status != StatusSuccess {
			t.Errorf("UpdateStatus() status = %s, want %s", event.Status, StatusSuccess)
		}
	})
}

func TestGetEventsByStatus(t *testing.T) {
	t.Run("get events by status", func(t *testing.T) {
		s := setupTestStore(t)
		createTestEvent(t, s, "pending-1", 100, StatusPending, 200)
		createTestEvent(t, s, "pending-2", 101, StatusPending, 200)
		createTestEvent(t, s, "success-1", 102, StatusSuccess, 200)
		createTestEvent(t, s, "expired-1", 103, StatusExpired, 200)

		events, err := s.GetEventsByStatus(StatusPending, 0)
		if err != nil {
			t.Fatalf("GetEventsByStatus() error = %v, want nil", err)
		}
		if len(events) != 2 {
			t.Errorf("GetEventsByStatus() returned %d events, want 2", len(events))
		}
		// Should be ordered by created_at DESC
		if events[0].EventID != "pending-2" {
			t.Errorf("GetEventsByStatus() first event ID = %s, want pending-2", events[0].EventID)
		}
		if events[1].EventID != "pending-1" {
			t.Errorf("GetEventsByStatus() second event ID = %s, want pending-1", events[1].EventID)
		}
	})

	t.Run("get events with limit", func(t *testing.T) {
		s := setupTestStore(t)
		createTestEvent(t, s, "pending-1", 100, StatusPending, 200)
		createTestEvent(t, s, "pending-2", 101, StatusPending, 200)
		createTestEvent(t, s, "pending-3", 102, StatusPending, 200)

		events, err := s.GetEventsByStatus(StatusPending, 2)
		if err != nil {
			t.Fatalf("GetEventsByStatus() error = %v, want nil", err)
		}
		if len(events) != 2 {
			t.Errorf("GetEventsByStatus() returned %d events, want 2", len(events))
		}
	})

	t.Run("no events with status", func(t *testing.T) {
		s := setupTestStore(t)
		createTestEvent(t, s, "pending-1", 100, StatusPending, 200)

		events, err := s.GetEventsByStatus(StatusSuccess, 0)
		if err != nil {
			t.Fatalf("GetEventsByStatus() error = %v, want nil", err)
		}
		if len(events) != 0 {
			t.Errorf("GetEventsByStatus() returned %d events, want 0", len(events))
		}
	})

	t.Run("limit zero returns all", func(t *testing.T) {
		s := setupTestStore(t)
		createTestEvent(t, s, "pending-1", 100, StatusPending, 200)
		createTestEvent(t, s, "pending-2", 101, StatusPending, 200)

		events, err := s.GetEventsByStatus(StatusPending, 0)
		if err != nil {
			t.Fatalf("GetEventsByStatus() error = %v, want nil", err)
		}
		if len(events) != 2 {
			t.Errorf("GetEventsByStatus() returned %d events, want 2", len(events))
		}
	})
}

func TestClearExpiredAndSuccessfulEvents(t *testing.T) {
	t.Run("clear both expired and successful events", func(t *testing.T) {
		s := setupTestStore(t)
		createTestEvent(t, s, "success-1", 100, StatusSuccess, 200)
		createTestEvent(t, s, "expired-1", 101, StatusExpired, 200)
		createTestEvent(t, s, "pending-1", 102, StatusPending, 200)
		createTestEvent(t, s, "in-progress-1", 103, StatusInProgress, 200)

		deleted, err := s.ClearExpiredAndSuccessfulEvents()
		if err != nil {
			t.Fatalf("ClearExpiredAndSuccessfulEvents() error = %v, want nil", err)
		}
		if deleted != 2 {
			t.Errorf("ClearExpiredAndSuccessfulEvents() deleted %d events, want 2", deleted)
		}

		// Verify both types are gone
		success, _ := s.GetEventsByStatus(StatusSuccess, 0)
		if len(success) != 0 {
			t.Errorf("GetEventsByStatus(StatusSuccess) returned %d events, want 0", len(success))
		}
		expired, _ := s.GetEventsByStatus(StatusExpired, 0)
		if len(expired) != 0 {
			t.Errorf("GetEventsByStatus(StatusExpired) returned %d events, want 0", len(expired))
		}

		// Verify other events still exist
		pending, _ := s.GetEventsByStatus(StatusPending, 0)
		if len(pending) != 1 {
			t.Errorf("GetEventsByStatus(StatusPending) returned %d events, want 1", len(pending))
		}
		inProgress, _ := s.GetEventsByStatus(StatusInProgress, 0)
		if len(inProgress) != 1 {
			t.Errorf("GetEventsByStatus(StatusInProgress) returned %d events, want 1", len(inProgress))
		}
	})
}
