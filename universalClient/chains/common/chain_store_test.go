package common

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/universalClient/db"
	storemodels "github.com/pushchain/push-chain-node/universalClient/store"
)

func TestNewChainStore(t *testing.T) {
	t.Run("creates chain store with nil database", func(t *testing.T) {
		store := NewChainStore(nil)
		require.NotNil(t, store)
		assert.Nil(t, store.database)
	})
}

func TestChainStoreNilDatabase(t *testing.T) {
	store := NewChainStore(nil)

	t.Run("GetChainHeight returns error for nil database", func(t *testing.T) {
		height, err := store.GetChainHeight()
		require.Error(t, err)
		assert.Equal(t, uint64(0), height)
		assert.Contains(t, err.Error(), "database is nil")
	})

	t.Run("UpdateChainHeight returns error for nil database", func(t *testing.T) {
		err := store.UpdateChainHeight(100)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "database is nil")
	})

	t.Run("GetPendingEvents returns error for nil database", func(t *testing.T) {
		events, err := store.GetPendingEvents(10)
		require.Error(t, err)
		assert.Nil(t, events)
		assert.Contains(t, err.Error(), "database is nil")
	})

	t.Run("GetConfirmedEvents returns error for nil database", func(t *testing.T) {
		events, err := store.GetConfirmedEvents(10)
		require.Error(t, err)
		assert.Nil(t, events)
		assert.Contains(t, err.Error(), "database is nil")
	})

	t.Run("UpdateEventStatus returns error for nil database", func(t *testing.T) {
		rowsAffected, err := store.UpdateEventStatus("event-1", storemodels.StatusPending, storemodels.StatusConfirmed)
		require.Error(t, err)
		assert.Equal(t, int64(0), rowsAffected)
		assert.Contains(t, err.Error(), "database is nil")
	})

	t.Run("UpdateVoteTxHash returns error for nil database", func(t *testing.T) {
		err := store.UpdateVoteTxHash("event-1", "0x123")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "database is nil")
	})

	t.Run("InsertEventIfNotExists returns error for nil database", func(t *testing.T) {
		inserted, err := store.InsertEventIfNotExists(nil)
		require.Error(t, err)
		assert.False(t, inserted)
		assert.Contains(t, err.Error(), "database is nil")
	})
}

func newTestChainStore(t *testing.T) *ChainStore {
	t.Helper()
	testDB, err := db.OpenInMemoryDB(true)
	require.NoError(t, err)
	t.Cleanup(func() { testDB.Close() })
	return NewChainStore(testDB)
}

func TestChainStore_GetChainHeight(t *testing.T) {
	cs := newTestChainStore(t)

	t.Run("creates state on first call", func(t *testing.T) {
		height, err := cs.GetChainHeight()
		require.NoError(t, err)
		assert.Equal(t, uint64(0), height)
	})

	t.Run("returns existing height", func(t *testing.T) {
		require.NoError(t, cs.UpdateChainHeight(100))
		height, err := cs.GetChainHeight()
		require.NoError(t, err)
		assert.Equal(t, uint64(100), height)
	})
}

func TestChainStore_UpdateChainHeight(t *testing.T) {
	cs := newTestChainStore(t)

	t.Run("creates and updates", func(t *testing.T) {
		require.NoError(t, cs.UpdateChainHeight(50))
		height, err := cs.GetChainHeight()
		require.NoError(t, err)
		assert.Equal(t, uint64(50), height)
	})

	t.Run("only updates if higher", func(t *testing.T) {
		require.NoError(t, cs.UpdateChainHeight(100))
		require.NoError(t, cs.UpdateChainHeight(50)) // lower — ignored
		height, err := cs.GetChainHeight()
		require.NoError(t, err)
		assert.Equal(t, uint64(100), height)
	})
}

func TestChainStore_InsertAndQuery(t *testing.T) {
	cs := newTestChainStore(t)

	event := &storemodels.Event{
		EventID:          "evt-1",
		BlockHeight:      10,
		Type:             storemodels.EventTypeInbound,
		ConfirmationType: storemodels.ConfirmationStandard,
		Status:           storemodels.StatusPending,
	}

	t.Run("insert new event", func(t *testing.T) {
		inserted, err := cs.InsertEventIfNotExists(event)
		require.NoError(t, err)
		assert.True(t, inserted)
	})

	t.Run("duplicate insert returns false", func(t *testing.T) {
		inserted, err := cs.InsertEventIfNotExists(event)
		require.NoError(t, err)
		assert.False(t, inserted)
	})

	t.Run("get pending events", func(t *testing.T) {
		events, err := cs.GetPendingEvents(10)
		require.NoError(t, err)
		require.Len(t, events, 1)
		assert.Equal(t, "evt-1", events[0].EventID)
	})

	t.Run("get confirmed events returns empty", func(t *testing.T) {
		events, err := cs.GetConfirmedEvents(10)
		require.NoError(t, err)
		assert.Empty(t, events)
	})
}

func TestChainStore_UpdateEventStatus(t *testing.T) {
	cs := newTestChainStore(t)

	event := &storemodels.Event{
		EventID:          "evt-2",
		BlockHeight:      20,
		Type:             storemodels.EventTypeInbound,
		ConfirmationType: storemodels.ConfirmationStandard,
		Status:           storemodels.StatusPending,
	}
	_, err := cs.InsertEventIfNotExists(event)
	require.NoError(t, err)

	t.Run("updates matching status", func(t *testing.T) {
		rows, err := cs.UpdateEventStatus("evt-2", storemodels.StatusPending, storemodels.StatusConfirmed)
		require.NoError(t, err)
		assert.Equal(t, int64(1), rows)
	})

	t.Run("no-op if status mismatch", func(t *testing.T) {
		rows, err := cs.UpdateEventStatus("evt-2", storemodels.StatusPending, storemodels.StatusCompleted)
		require.NoError(t, err)
		assert.Equal(t, int64(0), rows)
	})

	t.Run("confirmed events visible", func(t *testing.T) {
		events, err := cs.GetConfirmedEvents(10)
		require.NoError(t, err)
		require.Len(t, events, 1)
		assert.Equal(t, "evt-2", events[0].EventID)
	})
}

func TestChainStore_UpdateStatusAndVoteTxHash(t *testing.T) {
	cs := newTestChainStore(t)

	event := &storemodels.Event{
		EventID:          "evt-3",
		BlockHeight:      30,
		Type:             storemodels.EventTypeInbound,
		ConfirmationType: storemodels.ConfirmationStandard,
		Status:           storemodels.StatusConfirmed,
	}
	_, err := cs.InsertEventIfNotExists(event)
	require.NoError(t, err)

	rows, err := cs.UpdateStatusAndVoteTxHash("evt-3", storemodels.StatusConfirmed, storemodels.StatusCompleted, "0xvote123")
	require.NoError(t, err)
	assert.Equal(t, int64(1), rows)
}

func TestChainStore_UpdateStatusAndEventData(t *testing.T) {
	cs := newTestChainStore(t)

	event := &storemodels.Event{
		EventID:          "evt-4",
		BlockHeight:      40,
		Type:             storemodels.EventTypeInbound,
		ConfirmationType: storemodels.ConfirmationStandard,
		Status:           storemodels.StatusPending,
		EventData:        []byte(`{"old":"data"}`),
	}
	_, err := cs.InsertEventIfNotExists(event)
	require.NoError(t, err)

	newData := []byte(`{"new":"data"}`)
	rows, err := cs.UpdateStatusAndEventData("evt-4", storemodels.StatusPending, storemodels.StatusConfirmed, newData)
	require.NoError(t, err)
	assert.Equal(t, int64(1), rows)
}

func TestChainStore_UpdateVoteTxHash(t *testing.T) {
	cs := newTestChainStore(t)

	event := &storemodels.Event{
		EventID:          "evt-5",
		BlockHeight:      50,
		Type:             storemodels.EventTypeOutbound,
		ConfirmationType: storemodels.ConfirmationStandard,
		Status:           storemodels.StatusConfirmed,
	}
	_, err := cs.InsertEventIfNotExists(event)
	require.NoError(t, err)

	err = cs.UpdateVoteTxHash("evt-5", "0xvotehash")
	require.NoError(t, err)
}

func TestChainStore_GetPendingEventsLimit(t *testing.T) {
	cs := newTestChainStore(t)

	// Insert 5 pending events
	for i := 0; i < 5; i++ {
		evt := &storemodels.Event{
			EventID:          fmt.Sprintf("limit-evt-%d", i),
			BlockHeight:      uint64(i + 1),
			Type:             storemodels.EventTypeInbound,
			ConfirmationType: storemodels.ConfirmationStandard,
			Status:           storemodels.StatusPending,
		}
		inserted, err := cs.InsertEventIfNotExists(evt)
		require.NoError(t, err)
		assert.True(t, inserted)
	}

	t.Run("limit returns at most N events", func(t *testing.T) {
		events, err := cs.GetPendingEvents(3)
		require.NoError(t, err)
		assert.Len(t, events, 3)
	})

	t.Run("limit larger than total returns all", func(t *testing.T) {
		events, err := cs.GetPendingEvents(100)
		require.NoError(t, err)
		assert.Len(t, events, 5)
	})

	t.Run("limit zero returns empty", func(t *testing.T) {
		events, err := cs.GetPendingEvents(0)
		require.NoError(t, err)
		assert.Len(t, events, 0)
	})

	t.Run("limit one returns exactly one", func(t *testing.T) {
		events, err := cs.GetPendingEvents(1)
		require.NoError(t, err)
		assert.Len(t, events, 1)
	})
}

func TestChainStore_GetConfirmedEventsOrdering(t *testing.T) {
	cs := newTestChainStore(t)

	// Insert events in reverse order; they should come back ordered by created_at ASC
	for i := 4; i >= 0; i-- {
		evt := &storemodels.Event{
			EventID:          fmt.Sprintf("order-evt-%d", i),
			BlockHeight:      uint64(i + 1),
			Type:             storemodels.EventTypeInbound,
			ConfirmationType: storemodels.ConfirmationStandard,
			Status:           storemodels.StatusConfirmed,
		}
		inserted, err := cs.InsertEventIfNotExists(evt)
		require.NoError(t, err)
		assert.True(t, inserted)
	}

	t.Run("events ordered by created_at ASC", func(t *testing.T) {
		events, err := cs.GetConfirmedEvents(10)
		require.NoError(t, err)
		require.Len(t, events, 5)
		// Since they are inserted sequentially, created_at is monotonically increasing
		// The first inserted (i=4) has the earliest created_at
		assert.Equal(t, "order-evt-4", events[0].EventID)
		assert.Equal(t, "order-evt-0", events[4].EventID)
	})

	t.Run("limit constrains confirmed events", func(t *testing.T) {
		events, err := cs.GetConfirmedEvents(2)
		require.NoError(t, err)
		assert.Len(t, events, 2)
	})
}

func TestChainStore_DeleteTerminalEventsNilDatabase(t *testing.T) {
	cs := NewChainStore(nil)

	deleted, err := cs.DeleteTerminalEvents("2099-01-01")
	require.Error(t, err)
	assert.Equal(t, int64(0), deleted)
	assert.Contains(t, err.Error(), "database is nil")
}

func TestChainStore_DeleteTerminalEventsBoundary(t *testing.T) {
	cs := newTestChainStore(t)

	// Insert terminal events
	for i, status := range []string{storemodels.StatusCompleted, storemodels.StatusReverted, storemodels.StatusReorged} {
		evt := &storemodels.Event{
			EventID:          fmt.Sprintf("boundary-term-%d", i),
			BlockHeight:      uint64(i),
			Type:             storemodels.EventTypeInbound,
			ConfirmationType: storemodels.ConfirmationStandard,
			Status:           status,
		}
		_, err := cs.InsertEventIfNotExists(evt)
		require.NoError(t, err)
	}

	// Also insert a pending event (non-terminal)
	pending := &storemodels.Event{
		EventID:          "boundary-pending",
		BlockHeight:      100,
		Type:             storemodels.EventTypeInbound,
		ConfirmationType: storemodels.ConfirmationStandard,
		Status:           storemodels.StatusPending,
	}
	_, err := cs.InsertEventIfNotExists(pending)
	require.NoError(t, err)

	// Also insert a confirmed event (non-terminal)
	confirmed := &storemodels.Event{
		EventID:          "boundary-confirmed",
		BlockHeight:      101,
		Type:             storemodels.EventTypeInbound,
		ConfirmationType: storemodels.ConfirmationStandard,
		Status:           storemodels.StatusConfirmed,
	}
	_, err = cs.InsertEventIfNotExists(confirmed)
	require.NoError(t, err)

	t.Run("delete with past date deletes nothing", func(t *testing.T) {
		deleted, err := cs.DeleteTerminalEvents("2000-01-01")
		require.NoError(t, err)
		assert.Equal(t, int64(0), deleted)
	})

	t.Run("delete with future date only deletes terminal events", func(t *testing.T) {
		deleted, err := cs.DeleteTerminalEvents("2099-01-01")
		require.NoError(t, err)
		assert.Equal(t, int64(3), deleted)

		// Pending and confirmed events remain
		pendingEvents, err := cs.GetPendingEvents(10)
		require.NoError(t, err)
		assert.Len(t, pendingEvents, 1)

		confirmedEvents, err := cs.GetConfirmedEvents(10)
		require.NoError(t, err)
		assert.Len(t, confirmedEvents, 1)
	})
}

func TestChainStore_UpdateStatusAndVoteTxHashNilDatabase(t *testing.T) {
	cs := NewChainStore(nil)

	rows, err := cs.UpdateStatusAndVoteTxHash("evt-x", storemodels.StatusConfirmed, storemodels.StatusCompleted, "0xhash")
	require.Error(t, err)
	assert.Equal(t, int64(0), rows)
	assert.Contains(t, err.Error(), "database is nil")
}

func TestChainStore_UpdateStatusAndEventDataNilDatabase(t *testing.T) {
	cs := NewChainStore(nil)

	rows, err := cs.UpdateStatusAndEventData("evt-x", storemodels.StatusPending, storemodels.StatusConfirmed, []byte(`{}`))
	require.Error(t, err)
	assert.Equal(t, int64(0), rows)
	assert.Contains(t, err.Error(), "database is nil")
}

func TestChainStore_DeleteTerminalEvents(t *testing.T) {
	cs := newTestChainStore(t)

	// Insert events in terminal states
	for i, status := range []string{storemodels.StatusCompleted, storemodels.StatusReverted, storemodels.StatusReorged} {
		evt := &storemodels.Event{
			EventID:          fmt.Sprintf("term-%d", i),
			BlockHeight:      uint64(i),
			Type:             storemodels.EventTypeInbound,
			ConfirmationType: storemodels.ConfirmationStandard,
			Status:           status,
		}
		_, err := cs.InsertEventIfNotExists(evt)
		require.NoError(t, err)
	}

	// Insert a non-terminal event
	active := &storemodels.Event{
		EventID:          "active-1",
		BlockHeight:      100,
		Type:             storemodels.EventTypeInbound,
		ConfirmationType: storemodels.ConfirmationStandard,
		Status:           storemodels.StatusPending,
	}
	_, err := cs.InsertEventIfNotExists(active)
	require.NoError(t, err)

	// Delete terminal events updated before far future
	deleted, err := cs.DeleteTerminalEvents("2099-01-01")
	require.NoError(t, err)
	assert.Equal(t, int64(3), deleted)

	// Active event still exists
	events, err := cs.GetPendingEvents(10)
	require.NoError(t, err)
	assert.Len(t, events, 1)
	assert.Equal(t, "active-1", events[0].EventID)
}
