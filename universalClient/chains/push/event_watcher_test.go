package push

import (
	"context"
	"testing"
	"time"

	"github.com/pushchain/push-chain-node/universalClient/pushcore"
	"github.com/pushchain/push-chain-node/universalClient/store"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func newTestDBForWatcher(t *testing.T) *gorm.DB {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	require.NoError(t, err)

	err = db.AutoMigrate(&store.ChainState{}, &store.PCEvent{})
	require.NoError(t, err)

	return db
}

type mockPushClientForWatcher struct {
	latestBlock  uint64
	txResults    map[string][]*pushcore.TxResult // query -> results
	getBlockErr  error
	getTxsErr    error
	queriesMade  []string
}

func (m *mockPushClientForWatcher) GetLatestBlockNum() (uint64, error) {
	return m.latestBlock, m.getBlockErr
}

func (m *mockPushClientForWatcher) GetTxsByEvents(query string, minHeight, maxHeight uint64, limit uint64) ([]*pushcore.TxResult, error) {
	m.queriesMade = append(m.queriesMade, query)
	if m.getTxsErr != nil {
		return nil, m.getTxsErr
	}
	if results, ok := m.txResults[query]; ok {
		return results, nil
	}
	return nil, nil
}

func TestNewEventWatcher(t *testing.T) {
	logger := zerolog.Nop()
	db := newTestDBForWatcher(t)
	client := &mockPushClientForWatcher{}
	cfg := Config{
		PollInterval: 5 * time.Second,
		ChunkSize:    1000,
		QueryLimit:   100,
	}

	watcher := NewEventWatcher(client, db, logger, cfg, 100)

	require.NotNil(t, watcher)
	assert.Equal(t, uint64(100), watcher.LastProcessedBlock())
}

func TestEventWatcher_StartStop(t *testing.T) {
	logger := zerolog.Nop()
	db := newTestDBForWatcher(t)
	client := &mockPushClientForWatcher{latestBlock: 100}
	cfg := Config{
		PollInterval: 100 * time.Millisecond,
		ChunkSize:    1000,
		QueryLimit:   100,
	}

	watcher := NewEventWatcher(client, db, logger, cfg, 100)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := watcher.Start(ctx)
	require.NoError(t, err)

	// Give it time to start
	time.Sleep(50 * time.Millisecond)

	watcher.Stop()

	// Verify it can be stopped multiple times without issue
	watcher.Stop()
}

func TestEventWatcher_LastProcessedBlock(t *testing.T) {
	logger := zerolog.Nop()
	db := newTestDBForWatcher(t)
	client := &mockPushClientForWatcher{latestBlock: 100}
	cfg := Config{
		PollInterval: 5 * time.Second,
		ChunkSize:    1000,
		QueryLimit:   100,
	}

	watcher := NewEventWatcher(client, db, logger, cfg, 50)
	assert.Equal(t, uint64(50), watcher.LastProcessedBlock())
}

func TestEventWatcher_StoreEvent(t *testing.T) {
	logger := zerolog.Nop()
	db := newTestDBForWatcher(t)
	client := &mockPushClientForWatcher{latestBlock: 100}
	cfg := Config{
		PollInterval: 5 * time.Second,
		ChunkSize:    1000,
		QueryLimit:   100,
	}

	watcher := NewEventWatcher(client, db, logger, cfg, 0)

	t.Run("store new event", func(t *testing.T) {
		event := &store.PCEvent{
			EventID:     "test-event-1",
			TxHash:      "0xhash1",
			BlockHeight: 100,
			Type:        ProtocolTypeSign,
			Status:      StatusPending,
			EventData:   []byte(`{"test": "data"}`),
		}

		stored, err := watcher.storeEvent(event)
		require.NoError(t, err)
		assert.True(t, stored)

		// Verify it's in the database
		var found store.PCEvent
		err = db.Where("event_id = ?", "test-event-1").First(&found).Error
		require.NoError(t, err)
		assert.Equal(t, "test-event-1", found.EventID)
	})

	t.Run("skip duplicate event", func(t *testing.T) {
		event := &store.PCEvent{
			EventID:     "test-event-1", // Same as above
			TxHash:      "0xhash2",
			BlockHeight: 101,
			Type:        ProtocolTypeSign,
			Status:      StatusPending,
		}

		stored, err := watcher.storeEvent(event)
		require.NoError(t, err)
		assert.False(t, stored, "duplicate event should not be stored")
	})

	t.Run("store different event", func(t *testing.T) {
		event := &store.PCEvent{
			EventID:     "test-event-2",
			TxHash:      "0xhash3",
			BlockHeight: 102,
			Type:        ProtocolTypeKeygen,
			Status:      StatusPending,
		}

		stored, err := watcher.storeEvent(event)
		require.NoError(t, err)
		assert.True(t, stored)
	})
}

func TestEventWatcher_PersistBlockProgress(t *testing.T) {
	logger := zerolog.Nop()
	db := newTestDBForWatcher(t)
	client := &mockPushClientForWatcher{latestBlock: 100}
	cfg := Config{
		PollInterval: 5 * time.Second,
		ChunkSize:    1000,
		QueryLimit:   100,
	}

	watcher := NewEventWatcher(client, db, logger, cfg, 0)

	t.Run("create new chain state", func(t *testing.T) {
		err := watcher.persistBlockProgress(100)
		require.NoError(t, err)

		var state store.ChainState
		err = db.First(&state).Error
		require.NoError(t, err)
		assert.Equal(t, uint64(100), state.LastBlock)
	})

	t.Run("update existing chain state", func(t *testing.T) {
		err := watcher.persistBlockProgress(200)
		require.NoError(t, err)

		var state store.ChainState
		err = db.First(&state).Error
		require.NoError(t, err)
		assert.Equal(t, uint64(200), state.LastBlock)
	})

	t.Run("skip update if not progressed", func(t *testing.T) {
		err := watcher.persistBlockProgress(150) // Less than 200
		require.NoError(t, err)

		var state store.ChainState
		err = db.First(&state).Error
		require.NoError(t, err)
		assert.Equal(t, uint64(200), state.LastBlock) // Should still be 200
	})
}

func TestEventWatcher_QueryAndStoreEvents(t *testing.T) {
	logger := zerolog.Nop()
	db := newTestDBForWatcher(t)
	client := &mockPushClientForWatcher{
		latestBlock: 100,
		txResults:   make(map[string][]*pushcore.TxResult),
	}
	cfg := Config{
		PollInterval: 5 * time.Second,
		ChunkSize:    1000,
		QueryLimit:   100,
	}

	watcher := NewEventWatcher(client, db, logger, cfg, 0)

	t.Run("no results", func(t *testing.T) {
		count, err := watcher.queryAndStoreEvents(TSSEventQuery, 1, 100, "TSS")
		require.NoError(t, err)
		assert.Equal(t, 0, count)
	})
}

func TestEventWatcher_ProcessChunk(t *testing.T) {
	logger := zerolog.Nop()
	db := newTestDBForWatcher(t)
	client := &mockPushClientForWatcher{
		latestBlock: 100,
		txResults:   make(map[string][]*pushcore.TxResult),
	}
	cfg := Config{
		PollInterval: 5 * time.Second,
		ChunkSize:    1000,
		QueryLimit:   100,
	}

	watcher := NewEventWatcher(client, db, logger, cfg, 0)

	t.Run("processes both TSS and outbound queries", func(t *testing.T) {
		client.queriesMade = nil // Reset

		_, err := watcher.processChunk(1, 100)
		require.NoError(t, err)

		// Should have made two queries - one for TSS, one for outbound
		assert.Len(t, client.queriesMade, 2)
		assert.Contains(t, client.queriesMade, TSSEventQuery)
		assert.Contains(t, client.queriesMade, OutboundEventQuery)
	})
}

func TestEventWatcher_PollForEvents_CaughtUp(t *testing.T) {
	logger := zerolog.Nop()
	db := newTestDBForWatcher(t)
	client := &mockPushClientForWatcher{
		latestBlock: 100,
		txResults:   make(map[string][]*pushcore.TxResult),
	}
	cfg := Config{
		PollInterval: 5 * time.Second,
		ChunkSize:    1000,
		QueryLimit:   100,
	}

	// Start at block 100, latest is 100 - should be caught up
	watcher := NewEventWatcher(client, db, logger, cfg, 100)

	err := watcher.pollForEvents()
	require.NoError(t, err)

	// No queries should have been made since we're caught up
	assert.Len(t, client.queriesMade, 0)
}

func TestEventWatcher_PollForEvents_ProcessesNewBlocks(t *testing.T) {
	logger := zerolog.Nop()
	db := newTestDBForWatcher(t)
	client := &mockPushClientForWatcher{
		latestBlock: 200,
		txResults:   make(map[string][]*pushcore.TxResult),
	}
	cfg := Config{
		PollInterval: 5 * time.Second,
		ChunkSize:    1000,
		QueryLimit:   100,
	}

	// Start at block 100, latest is 200 - should process new blocks
	watcher := NewEventWatcher(client, db, logger, cfg, 100)

	// Need to start the watcher to initialize context (then stop it to run manual poll)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	err := watcher.Start(ctx)
	require.NoError(t, err)
	watcher.Stop()

	// Re-create watcher for clean test
	client.queriesMade = nil
	watcher = NewEventWatcher(client, db, logger, cfg, 100)
	watcher.ctx, watcher.cancel = context.WithCancel(context.Background())
	defer watcher.cancel()

	err = watcher.pollForEvents()
	require.NoError(t, err)

	// Should have processed blocks and updated last block
	assert.Equal(t, uint64(200), watcher.LastProcessedBlock())

	// Should have made queries (2 per chunk: TSS + outbound)
	assert.GreaterOrEqual(t, len(client.queriesMade), 2)
}

func TestMin(t *testing.T) {
	tests := []struct {
		a, b     uint64
		expected uint64
	}{
		{1, 2, 1},
		{2, 1, 1},
		{0, 0, 0},
		{100, 100, 100},
		{0, 100, 0},
	}

	for _, tt := range tests {
		result := min(tt.a, tt.b)
		assert.Equal(t, tt.expected, result)
	}
}

func TestEventQueries(t *testing.T) {
	// Verify query constants are correctly formed
	assert.Equal(t, "tss_process_initiated.process_id>=0", TSSEventQuery)
	assert.Equal(t, "outbound_created.tx_id EXISTS", OutboundEventQuery)
}

