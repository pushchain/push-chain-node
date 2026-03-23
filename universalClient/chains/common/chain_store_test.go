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
