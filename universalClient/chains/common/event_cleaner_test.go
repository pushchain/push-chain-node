package common

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ucdb "github.com/pushchain/push-chain-node/universalClient/db"
	storemodels "github.com/pushchain/push-chain-node/universalClient/store"
)

func TestNewEventCleaner(t *testing.T) {
	t.Run("creates event cleaner with valid params", func(t *testing.T) {
		logger := zerolog.Nop()
		cleanupInterval := 1 * time.Hour
		retentionPeriod := 24 * time.Hour
		chainID := "eip155:1"

		cleaner := NewEventCleaner(nil, cleanupInterval, retentionPeriod, chainID, logger)

		require.NotNil(t, cleaner)
		assert.Equal(t, cleanupInterval, cleaner.cleanupInterval)
		assert.Equal(t, retentionPeriod, cleaner.retentionPeriod)
		assert.Nil(t, cleaner.database)
		assert.NotNil(t, cleaner.stopCh)
	})

	t.Run("creates event cleaner with different intervals", func(t *testing.T) {
		logger := zerolog.Nop()

		testCases := []struct {
			cleanup   time.Duration
			retention time.Duration
		}{
			{30 * time.Minute, 12 * time.Hour},
			{1 * time.Hour, 48 * time.Hour},
			{5 * time.Minute, 1 * time.Hour},
		}

		for _, tc := range testCases {
			cleaner := NewEventCleaner(nil, tc.cleanup, tc.retention, "test-chain", logger)
			assert.Equal(t, tc.cleanup, cleaner.cleanupInterval)
			assert.Equal(t, tc.retention, cleaner.retentionPeriod)
		}
	})
}

func TestEventCleanerStruct(t *testing.T) {
	t.Run("struct has expected fields", func(t *testing.T) {
		ec := &EventCleaner{}
		assert.Nil(t, ec.database)
		assert.Equal(t, time.Duration(0), ec.cleanupInterval)
		assert.Equal(t, time.Duration(0), ec.retentionPeriod)
		assert.Nil(t, ec.ticker)
		assert.Nil(t, ec.stopCh)
	})
}

func TestEventCleanerStop(t *testing.T) {
	t.Run("stop closes channel", func(t *testing.T) {
		logger := zerolog.Nop()
		cleaner := NewEventCleaner(nil, time.Hour, time.Hour, "test-chain", logger)

		// Start a ticker to test stop
		cleaner.ticker = time.NewTicker(time.Hour)

		// Should not panic
		cleaner.Stop()
	})

	t.Run("stop with nil ticker", func(t *testing.T) {
		logger := zerolog.Nop()
		cleaner := NewEventCleaner(nil, time.Hour, time.Hour, "test-chain", logger)
		cleaner.ticker = nil

		// Should not panic
		cleaner.Stop()
	})
}

// newTestCleanerDB creates an in-memory database with optional seed events.
func newTestCleanerDB(t *testing.T, events []storemodels.Event) *ucdb.DB {
	t.Helper()
	database, err := ucdb.OpenInMemoryDB(true)
	require.NoError(t, err)
	t.Cleanup(func() { database.Close() })

	for _, e := range events {
		result := database.Client().Create(&e)
		require.NoError(t, result.Error)
	}
	return database
}

func TestPerformCleanup(t *testing.T) {
	t.Run("deletes terminal events older than retention period", func(t *testing.T) {
		database := newTestCleanerDB(t, nil)
		logger := zerolog.Nop()

		// Insert terminal events: COMPLETED, REVERTED, REORGED
		for i, status := range []string{storemodels.StatusCompleted, storemodels.StatusReverted, storemodels.StatusReorged} {
			evt := storemodels.Event{
				EventID:          fmt.Sprintf("terminal-%d", i),
				BlockHeight:      uint64(i),
				Type:             storemodels.EventTypeInbound,
				ConfirmationType: storemodels.ConfirmationStandard,
				Status:           status,
			}
			result := database.Client().Create(&evt)
			require.NoError(t, result.Error)
		}

		// Also insert a PENDING event that should NOT be deleted
		pending := storemodels.Event{
			EventID:          "pending-1",
			BlockHeight:      100,
			Type:             storemodels.EventTypeInbound,
			ConfirmationType: storemodels.ConfirmationStandard,
			Status:           storemodels.StatusPending,
		}
		result := database.Client().Create(&pending)
		require.NoError(t, result.Error)

		// Use zero retention period so all terminal events are eligible for cleanup
		cleaner := NewEventCleaner(database, time.Hour, 0, "test-chain", logger)

		err := cleaner.performCleanup()
		require.NoError(t, err)

		// Verify terminal events are deleted
		var remaining []storemodels.Event
		database.Client().Find(&remaining)
		require.Len(t, remaining, 1)
		assert.Equal(t, "pending-1", remaining[0].EventID)
	})

	t.Run("does not delete events within retention period", func(t *testing.T) {
		database := newTestCleanerDB(t, nil)
		logger := zerolog.Nop()

		// Insert a terminal event (just created, so updated_at is now)
		evt := storemodels.Event{
			EventID:          "recent-completed",
			BlockHeight:      1,
			Type:             storemodels.EventTypeInbound,
			ConfirmationType: storemodels.ConfirmationStandard,
			Status:           storemodels.StatusCompleted,
		}
		result := database.Client().Create(&evt)
		require.NoError(t, result.Error)

		// Use a very long retention period so the event is still within retention
		cleaner := NewEventCleaner(database, time.Hour, 24*time.Hour, "test-chain", logger)

		err := cleaner.performCleanup()
		require.NoError(t, err)

		// Event should still exist
		var remaining []storemodels.Event
		database.Client().Find(&remaining)
		assert.Len(t, remaining, 1)
	})

	t.Run("no events to delete returns no error", func(t *testing.T) {
		database := newTestCleanerDB(t, nil)
		logger := zerolog.Nop()

		cleaner := NewEventCleaner(database, time.Hour, 0, "test-chain", logger)

		err := cleaner.performCleanup()
		assert.NoError(t, err)
	})
}

func TestEventCleanerStart(t *testing.T) {
	t.Run("start runs initial cleanup and returns nil", func(t *testing.T) {
		// Seed a terminal event
		database := newTestCleanerDB(t, []storemodels.Event{
			{
				EventID:          "old-completed",
				BlockHeight:      1,
				Type:             storemodels.EventTypeInbound,
				ConfirmationType: storemodels.ConfirmationStandard,
				Status:           storemodels.StatusCompleted,
			},
		})
		logger := zerolog.Nop()

		// Zero retention so the initial cleanup deletes the event
		cleaner := NewEventCleaner(database, time.Hour, 0, "test-chain", logger)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		err := cleaner.Start(ctx)
		require.NoError(t, err)

		// Give initial cleanup a moment to complete (it runs synchronously before the goroutine)
		// The initial cleanup in Start is synchronous, so it should have already run.
		var remaining []storemodels.Event
		database.Client().Find(&remaining)
		assert.Empty(t, remaining, "initial cleanup should have deleted the terminal event")

		// Clean up
		cancel()
	})

	t.Run("start stops when context is cancelled", func(t *testing.T) {
		database := newTestCleanerDB(t, nil)
		logger := zerolog.Nop()

		cleaner := NewEventCleaner(database, 50*time.Millisecond, 0, "test-chain", logger)

		ctx, cancel := context.WithCancel(context.Background())

		err := cleaner.Start(ctx)
		require.NoError(t, err)
		require.NotNil(t, cleaner.ticker)

		// Cancel the context and give the goroutine time to exit
		cancel()
		time.Sleep(100 * time.Millisecond)
	})

	t.Run("start stops when Stop is called", func(t *testing.T) {
		database := newTestCleanerDB(t, nil)
		logger := zerolog.Nop()

		cleaner := NewEventCleaner(database, 50*time.Millisecond, 0, "test-chain", logger)

		ctx := context.Background()

		err := cleaner.Start(ctx)
		require.NoError(t, err)

		// Stop should cause the goroutine to exit
		cleaner.Stop()
		time.Sleep(100 * time.Millisecond)
	})

	t.Run("periodic cleanup runs on ticker interval", func(t *testing.T) {
		database := newTestCleanerDB(t, nil)
		logger := zerolog.Nop()

		// Use a very short interval so the ticker fires quickly
		cleaner := NewEventCleaner(database, 50*time.Millisecond, 0, "test-chain", logger)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		err := cleaner.Start(ctx)
		require.NoError(t, err)

		// Insert a terminal event after Start so it was not cleaned by initial cleanup
		evt := storemodels.Event{
			EventID:          "late-completed",
			BlockHeight:      1,
			Type:             storemodels.EventTypeInbound,
			ConfirmationType: storemodels.ConfirmationStandard,
			Status:           storemodels.StatusCompleted,
		}
		result := database.Client().Create(&evt)
		require.NoError(t, result.Error)

		// Wait for at least one ticker cycle
		time.Sleep(150 * time.Millisecond)

		var remaining []storemodels.Event
		database.Client().Find(&remaining)
		assert.Empty(t, remaining, "periodic cleanup should have deleted the terminal event")

		cancel()
	})
}

func TestEventCleanerStartStopLifecycle(t *testing.T) {
	t.Run("full lifecycle: start, cleanup, stop", func(t *testing.T) {
		// Seed terminal events
		database := newTestCleanerDB(t, []storemodels.Event{
			{
				EventID:          "completed-1",
				BlockHeight:      10,
				Type:             storemodels.EventTypeOutbound,
				ConfirmationType: storemodels.ConfirmationStandard,
				Status:           storemodels.StatusCompleted,
			},
			{
				EventID:          "reverted-1",
				BlockHeight:      20,
				Type:             storemodels.EventTypeInbound,
				ConfirmationType: storemodels.ConfirmationStandard,
				Status:           storemodels.StatusReverted,
			},
			{
				EventID:          "pending-keep",
				BlockHeight:      30,
				Type:             storemodels.EventTypeInbound,
				ConfirmationType: storemodels.ConfirmationStandard,
				Status:           storemodels.StatusPending,
			},
		})
		logger := zerolog.Nop()

		cleaner := NewEventCleaner(database, time.Hour, 0, "test-chain", logger)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Start: initial cleanup removes terminal events
		err := cleaner.Start(ctx)
		require.NoError(t, err)

		var remaining []storemodels.Event
		database.Client().Find(&remaining)
		require.Len(t, remaining, 1)
		assert.Equal(t, "pending-keep", remaining[0].EventID)

		// Stop gracefully
		cleaner.Stop()

		// After stop, cleaner should not panic or leave stale state
		time.Sleep(50 * time.Millisecond)
	})
}
