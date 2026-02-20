package common

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
		rowsAffected, err := store.UpdateEventStatus("event-1", "PENDING", "CONFIRMED")
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

func TestChainStoreStruct(t *testing.T) {
	t.Run("struct has database field", func(t *testing.T) {
		store := &ChainStore{}
		assert.Nil(t, store.database)
	})
}
