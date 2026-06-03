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

func intPtr(v int) *int { return &v }

func TestNewEventCleaner(t *testing.T) {
	t.Run("creates event cleaner with valid params", func(t *testing.T) {
		logger := zerolog.Nop()
		chainID := "eip155:1"

		cleaner := NewEventCleaner(nil, intPtr(3600), intPtr(86400), chainID, logger)

		require.NotNil(t, cleaner)
		assert.Equal(t, 1*time.Hour, cleaner.cleanupInterval)
		assert.Equal(t, 24*time.Hour, cleaner.retentionPeriod)
		assert.Nil(t, cleaner.database)
		// stopCh is created in Start, not at construction time.
		assert.Nil(t, cleaner.stopCh)
		assert.False(t, cleaner.running)
	})

	t.Run("nil pointers fall back to package defaults", func(t *testing.T) {
		cleaner := NewEventCleaner(nil, nil, nil, "test-chain", zerolog.Nop())
		require.NotNil(t, cleaner)
		assert.Equal(t, defaultCleanupInterval, cleaner.cleanupInterval)
		assert.Equal(t, defaultRetentionPeriod, cleaner.retentionPeriod)
	})

	t.Run("only one pointer set: other falls back to default", func(t *testing.T) {
		cleaner := NewEventCleaner(nil, intPtr(60), nil, "test-chain", zerolog.Nop())
		assert.Equal(t, 60*time.Second, cleaner.cleanupInterval)
		assert.Equal(t, defaultRetentionPeriod, cleaner.retentionPeriod)

		cleaner = NewEventCleaner(nil, nil, intPtr(60), "test-chain", zerolog.Nop())
		assert.Equal(t, defaultCleanupInterval, cleaner.cleanupInterval)
		assert.Equal(t, 60*time.Second, cleaner.retentionPeriod)
	})

	t.Run("creates event cleaner with different intervals", func(t *testing.T) {
		testCases := []struct {
			cleanupSec   int
			retentionSec int
			cleanup      time.Duration
			retention    time.Duration
		}{
			{1800, 43200, 30 * time.Minute, 12 * time.Hour},
			{3600, 172800, 1 * time.Hour, 48 * time.Hour},
			{300, 3600, 5 * time.Minute, 1 * time.Hour},
		}

		for _, tc := range testCases {
			cleaner := NewEventCleaner(nil, intPtr(tc.cleanupSec), intPtr(tc.retentionSec), "test-chain", zerolog.Nop())
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
	t.Run("stop before start is a no-op", func(t *testing.T) {
		cleaner := NewEventCleaner(nil, intPtr(3600), intPtr(3600), "test-chain", zerolog.Nop())
		// Must not panic on close(nil stopCh).
		cleaner.Stop()
	})

	t.Run("stop after start closes channel and flips running flag", func(t *testing.T) {
		database := newTestCleanerDB(t, nil)
		cleaner := NewEventCleaner(database, intPtr(3600), intPtr(0), "test-chain", zerolog.Nop())

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		require.NoError(t, cleaner.Start(ctx))
		require.True(t, cleaner.running)

		cleaner.Stop()
		assert.False(t, cleaner.running)
	})

	t.Run("double stop is idempotent", func(t *testing.T) {
		database := newTestCleanerDB(t, nil)
		cleaner := NewEventCleaner(database, intPtr(3600), intPtr(0), "test-chain", zerolog.Nop())

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		require.NoError(t, cleaner.Start(ctx))

		cleaner.Stop()
		cleaner.Stop() // must not panic on close(closed channel)
	})

	t.Run("restart after stop is supported", func(t *testing.T) {
		database := newTestCleanerDB(t, nil)
		cleaner := NewEventCleaner(database, intPtr(3600), intPtr(0), "test-chain", zerolog.Nop())

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		require.NoError(t, cleaner.Start(ctx))
		cleaner.Stop()
		require.NoError(t, cleaner.Start(ctx)) // stopCh recreated, no error
		cleaner.Stop()
	})

	t.Run("double start fails", func(t *testing.T) {
		database := newTestCleanerDB(t, nil)
		cleaner := NewEventCleaner(database, intPtr(3600), intPtr(0), "test-chain", zerolog.Nop())

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		require.NoError(t, cleaner.Start(ctx))
		err := cleaner.Start(ctx)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "already running")
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

		for i, status := range []string{storemodels.StatusCompleted, storemodels.StatusReverted, storemodels.StatusReorged} {
			evt := storemodels.Event{
				EventID:          fmt.Sprintf("terminal-%d", i),
				BlockHeight:      uint64(i),
				Type:             storemodels.EventTypeInbound,
				ConfirmationType: storemodels.ConfirmationStandard,
				Status:           status,
			}
			require.NoError(t, database.Client().Create(&evt).Error)
		}

		pending := storemodels.Event{
			EventID:          "pending-1",
			BlockHeight:      100,
			Type:             storemodels.EventTypeInbound,
			ConfirmationType: storemodels.ConfirmationStandard,
			Status:           storemodels.StatusPending,
		}
		require.NoError(t, database.Client().Create(&pending).Error)

		// Zero retention so all terminal events are eligible.
		cleaner := NewEventCleaner(database, intPtr(3600), intPtr(0), "test-chain", zerolog.Nop())

		require.NoError(t, cleaner.performCleanup())

		var remaining []storemodels.Event
		database.Client().Find(&remaining)
		require.Len(t, remaining, 1)
		assert.Equal(t, "pending-1", remaining[0].EventID)
	})

	t.Run("does not delete events within retention period", func(t *testing.T) {
		database := newTestCleanerDB(t, nil)

		evt := storemodels.Event{
			EventID:          "recent-completed",
			BlockHeight:      1,
			Type:             storemodels.EventTypeInbound,
			ConfirmationType: storemodels.ConfirmationStandard,
			Status:           storemodels.StatusCompleted,
		}
		require.NoError(t, database.Client().Create(&evt).Error)

		cleaner := NewEventCleaner(database, intPtr(3600), intPtr(86400), "test-chain", zerolog.Nop())

		require.NoError(t, cleaner.performCleanup())

		var remaining []storemodels.Event
		database.Client().Find(&remaining)
		assert.Len(t, remaining, 1)
	})

	t.Run("no events to delete returns no error", func(t *testing.T) {
		database := newTestCleanerDB(t, nil)
		cleaner := NewEventCleaner(database, intPtr(3600), intPtr(0), "test-chain", zerolog.Nop())
		assert.NoError(t, cleaner.performCleanup())
	})
}

func TestEventCleanerStart(t *testing.T) {
	t.Run("start runs initial cleanup and returns nil", func(t *testing.T) {
		database := newTestCleanerDB(t, []storemodels.Event{
			{
				EventID:          "old-completed",
				BlockHeight:      1,
				Type:             storemodels.EventTypeInbound,
				ConfirmationType: storemodels.ConfirmationStandard,
				Status:           storemodels.StatusCompleted,
			},
		})

		cleaner := NewEventCleaner(database, intPtr(3600), intPtr(0), "test-chain", zerolog.Nop())

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		require.NoError(t, cleaner.Start(ctx))

		var remaining []storemodels.Event
		database.Client().Find(&remaining)
		assert.Empty(t, remaining, "initial cleanup should have deleted the terminal event")

		cancel()
	})

	t.Run("start stops when context is cancelled", func(t *testing.T) {
		database := newTestCleanerDB(t, nil)
		cleaner := NewEventCleaner(database, intPtr(3600), intPtr(0), "test-chain", zerolog.Nop())
		// Override to a fast interval to keep the test snappy.
		cleaner.cleanupInterval = 50 * time.Millisecond

		ctx, cancel := context.WithCancel(context.Background())

		require.NoError(t, cleaner.Start(ctx))
		require.NotNil(t, cleaner.ticker)

		cancel()
		time.Sleep(100 * time.Millisecond)
	})

	t.Run("start stops when Stop is called", func(t *testing.T) {
		database := newTestCleanerDB(t, nil)
		cleaner := NewEventCleaner(database, intPtr(3600), intPtr(0), "test-chain", zerolog.Nop())
		cleaner.cleanupInterval = 50 * time.Millisecond

		require.NoError(t, cleaner.Start(context.Background()))
		cleaner.Stop()
		time.Sleep(100 * time.Millisecond)
	})

	t.Run("periodic cleanup runs on ticker interval", func(t *testing.T) {
		database := newTestCleanerDB(t, nil)
		cleaner := NewEventCleaner(database, intPtr(3600), intPtr(0), "test-chain", zerolog.Nop())
		cleaner.cleanupInterval = 50 * time.Millisecond

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		require.NoError(t, cleaner.Start(ctx))

		evt := storemodels.Event{
			EventID:          "late-completed",
			BlockHeight:      1,
			Type:             storemodels.EventTypeInbound,
			ConfirmationType: storemodels.ConfirmationStandard,
			Status:           storemodels.StatusCompleted,
		}
		require.NoError(t, database.Client().Create(&evt).Error)

		time.Sleep(150 * time.Millisecond)

		var remaining []storemodels.Event
		database.Client().Find(&remaining)
		assert.Empty(t, remaining, "periodic cleanup should have deleted the terminal event")

		cancel()
	})
}

func TestEventCleanerStartStopLifecycle(t *testing.T) {
	t.Run("full lifecycle: start, cleanup, stop", func(t *testing.T) {
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

		cleaner := NewEventCleaner(database, intPtr(3600), intPtr(0), "test-chain", zerolog.Nop())

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		require.NoError(t, cleaner.Start(ctx))

		var remaining []storemodels.Event
		database.Client().Find(&remaining)
		require.Len(t, remaining, 1)
		assert.Equal(t, "pending-keep", remaining[0].EventID)

		cleaner.Stop()
		time.Sleep(50 * time.Millisecond)
	})
}
