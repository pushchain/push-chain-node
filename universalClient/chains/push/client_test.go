package push

import (
	"context"
	"fmt"
	"testing"

	"github.com/pushchain/push-chain-node/universalClient/chains/common"
	"github.com/pushchain/push-chain-node/universalClient/config"
	"github.com/pushchain/push-chain-node/universalClient/db"
	"github.com/pushchain/push-chain-node/universalClient/pushcore"
	"github.com/pushchain/push-chain-node/universalClient/store"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestPushCoreClient creates a minimal pushcore.Client for testing.
// Empty slices — actual gRPC operations will fail but structure is valid.
func newTestPushCoreClient() *pushcore.Client {
	return &pushcore.Client{}
}

func newTestDB(t *testing.T) *db.DB {
	t.Helper()
	testDB, err := db.OpenInMemoryDB(true)
	require.NoError(t, err)
	return testDB
}

func TestNewClient(t *testing.T) {
	logger := zerolog.Nop()
	database := newTestDB(t)
	pc := newTestPushCoreClient()

	t.Run("success with nil config", func(t *testing.T) {
		client, err := NewClient(database, nil, pc, "push-chain", logger)
		require.NoError(t, err)
		require.NotNil(t, client)
		assert.NotNil(t, client.eventListener)
		assert.Nil(t, client.eventCleaner)
	})

	t.Run("success with event cleaner config", func(t *testing.T) {
		cleanup := 60
		retention := 3600
		cfg := &config.ChainSpecificConfig{
			CleanupIntervalSeconds: &cleanup,
			RetentionPeriodSeconds: &retention,
		}
		client, err := NewClient(database, cfg, pc, "push-chain", logger)
		require.NoError(t, err)
		require.NotNil(t, client)
		assert.NotNil(t, client.eventCleaner)
	})

	t.Run("nil pushcore fails", func(t *testing.T) {
		_, err := NewClient(database, nil, nil, "push-chain", logger)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "push client is nil")
	})

	t.Run("nil database fails", func(t *testing.T) {
		_, err := NewClient(nil, nil, pc, "push-chain", logger)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "database is nil")
	})
}

func TestClient_StartStop(t *testing.T) {
	client, err := NewClient(newTestDB(t), nil, newTestPushCoreClient(), "push-chain", zerolog.Nop())
	require.NoError(t, err)

	ctx := context.Background()

	require.NoError(t, client.Start(ctx))
	require.NoError(t, client.Stop())
}

func TestClient_IsHealthy(t *testing.T) {
	t.Run("nil pushcore returns false", func(t *testing.T) {
		client := &Client{logger: zerolog.Nop()}
		assert.False(t, client.IsHealthy())
	})

	t.Run("pushcore with no endpoints returns false", func(t *testing.T) {
		client := &Client{
			logger:   zerolog.Nop(),
			pushCore: newTestPushCoreClient(),
		}
		assert.False(t, client.IsHealthy())
	})
}

func TestClient_GetTxBuilder(t *testing.T) {
	client := &Client{logger: zerolog.Nop()}
	builder, err := client.GetTxBuilder()
	require.Error(t, err)
	assert.Nil(t, builder)
	assert.Contains(t, err.Error(), "not supported")
}

func TestClient_StopBeforeStart(t *testing.T) {
	// Stop on a freshly created client (never started) should not panic.
	// The cancel func is nil, eventListener.Stop() returns ErrNotRunning but
	// the client logs and swallows that error, returning nil.
	client, err := NewClient(newTestDB(t), nil, newTestPushCoreClient(), "push-chain", zerolog.Nop())
	require.NoError(t, err)

	// Should not panic or return error
	require.NoError(t, client.Stop())
}

func TestClient_DoubleStop(t *testing.T) {
	client, err := NewClient(newTestDB(t), nil, newTestPushCoreClient(), "push-chain", zerolog.Nop())
	require.NoError(t, err)

	ctx := context.Background()
	require.NoError(t, client.Start(ctx))
	require.NoError(t, client.Stop())

	// Second stop: eventListener.Stop() returns ErrNotRunning, but client swallows it
	require.NoError(t, client.Stop())
}

func TestClient_StartStopWithEventCleaner(t *testing.T) {
	cleanup := 60
	retention := 3600
	cfg := &config.ChainSpecificConfig{
		CleanupIntervalSeconds: &cleanup,
		RetentionPeriodSeconds: &retention,
	}
	client, err := NewClient(newTestDB(t), cfg, newTestPushCoreClient(), "push-chain", zerolog.Nop())
	require.NoError(t, err)
	require.NotNil(t, client.eventCleaner)

	ctx := context.Background()
	require.NoError(t, client.Start(ctx))

	// Verify both components are running
	assert.True(t, client.eventListener.IsRunning())

	require.NoError(t, client.Stop())

	// Verify event listener is stopped
	assert.False(t, client.eventListener.IsRunning())
}

func TestClient_StartStopLifecycleMultiple(t *testing.T) {
	// Verify the client can be started and stopped multiple times (restart).
	client, err := NewClient(newTestDB(t), nil, newTestPushCoreClient(), "push-chain", zerolog.Nop())
	require.NoError(t, err)

	ctx := context.Background()

	for i := 0; i < 3; i++ {
		require.NoError(t, client.Start(ctx), "start iteration %d", i)
		assert.True(t, client.eventListener.IsRunning(), "running after start iteration %d", i)
		require.NoError(t, client.Stop(), "stop iteration %d", i)
		assert.False(t, client.eventListener.IsRunning(), "not running after stop iteration %d", i)
	}
}

func TestNewClient_PartialCleanerConfig(t *testing.T) {
	logger := zerolog.Nop()
	database := newTestDB(t)
	pc := newTestPushCoreClient()

	t.Run("only cleanup interval set, no retention", func(t *testing.T) {
		cleanup := 60
		cfg := &config.ChainSpecificConfig{
			CleanupIntervalSeconds: &cleanup,
		}
		client, err := NewClient(database, cfg, pc, "push-chain", logger)
		require.NoError(t, err)
		assert.Nil(t, client.eventCleaner, "event cleaner should be nil when retention is missing")
	})

	t.Run("only retention set, no cleanup interval", func(t *testing.T) {
		retention := 3600
		cfg := &config.ChainSpecificConfig{
			RetentionPeriodSeconds: &retention,
		}
		client, err := NewClient(database, cfg, pc, "push-chain", logger)
		require.NoError(t, err)
		assert.Nil(t, client.eventCleaner, "event cleaner should be nil when cleanup interval is missing")
	})

	t.Run("empty config, no cleaner fields", func(t *testing.T) {
		cfg := &config.ChainSpecificConfig{}
		client, err := NewClient(database, cfg, pc, "push-chain", logger)
		require.NoError(t, err)
		assert.Nil(t, client.eventCleaner)
	})

	t.Run("config with poll interval but no cleaner", func(t *testing.T) {
		poll := 5
		cfg := &config.ChainSpecificConfig{
			EventPollingIntervalSeconds: &poll,
		}
		client, err := NewClient(database, cfg, pc, "push-chain", logger)
		require.NoError(t, err)
		assert.Nil(t, client.eventCleaner)
		assert.NotNil(t, client.eventListener)
	})
}

func TestNewClient_NegativePollInterval(t *testing.T) {
	logger := zerolog.Nop()
	database := newTestDB(t)
	pc := newTestPushCoreClient()

	poll := -5
	cfg := &config.ChainSpecificConfig{
		EventPollingIntervalSeconds: &poll,
	}
	client, err := NewClient(database, cfg, pc, "push-chain", logger)
	require.NoError(t, err)
	// Negative poll interval should fall back to default
	assert.Equal(t, DefaultPollInterval, client.eventListener.cfg.PollInterval)
}

// ---------------------------------------------------------------------------
// storeEvent tests
// ---------------------------------------------------------------------------

func TestStoreEvent(t *testing.T) {
	t.Run("stores a valid event and returns 1", func(t *testing.T) {
		database := newTestDB(t)
		pc := newTestPushCoreClient()
		logger := zerolog.Nop()

		el, err := NewEventListener(pc, database, logger, nil)
		require.NoError(t, err)

		event := &store.Event{
			EventID:          "test-event-1",
			BlockHeight:      100,
			Type:             store.EventTypeInbound,
			ConfirmationType: store.ConfirmationInstant,
			Status:           store.StatusConfirmed,
			EventData:        []byte(`{"key":"value"}`),
		}

		result := el.storeEvent(event)
		assert.Equal(t, 1, result)
	})

	t.Run("storing a duplicate event returns 0", func(t *testing.T) {
		database := newTestDB(t)
		pc := newTestPushCoreClient()
		logger := zerolog.Nop()

		el, err := NewEventListener(pc, database, logger, nil)
		require.NoError(t, err)

		event := &store.Event{
			EventID:          "test-event-dup",
			BlockHeight:      200,
			Type:             store.EventTypeOutbound,
			ConfirmationType: store.ConfirmationInstant,
			Status:           store.StatusConfirmed,
			EventData:        []byte(`{}`),
		}

		first := el.storeEvent(event)
		assert.Equal(t, 1, first)

		second := el.storeEvent(event)
		assert.Equal(t, 0, second)
	})

	t.Run("storing multiple distinct events returns 1 each", func(t *testing.T) {
		database := newTestDB(t)
		pc := newTestPushCoreClient()
		logger := zerolog.Nop()

		el, err := NewEventListener(pc, database, logger, nil)
		require.NoError(t, err)

		for i := 0; i < 5; i++ {
			event := &store.Event{
				EventID:          fmt.Sprintf("multi-event-%d", i),
				BlockHeight:      uint64(300 + i),
				Type:             store.EventTypeKeygen,
				ConfirmationType: store.ConfirmationInstant,
				Status:           store.StatusConfirmed,
				EventData:        []byte(`{}`),
			}
			result := el.storeEvent(event)
			assert.Equal(t, 1, result, "event %d should be stored", i)
		}
	})

	t.Run("stored event is retrievable via chainStore", func(t *testing.T) {
		database := newTestDB(t)
		pc := newTestPushCoreClient()
		logger := zerolog.Nop()

		el, err := NewEventListener(pc, database, logger, nil)
		require.NoError(t, err)

		event := &store.Event{
			EventID:          "retrievable-event",
			BlockHeight:      500,
			Type:             store.EventTypeInbound,
			ConfirmationType: store.ConfirmationInstant,
			Status:           store.StatusConfirmed,
			EventData:        []byte(`{"data":"test"}`),
		}

		result := el.storeEvent(event)
		assert.Equal(t, 1, result)

		// Verify it can be retrieved as a confirmed event
		cs := common.NewChainStore(database)
		confirmed, err := cs.GetConfirmedEvents(10)
		require.NoError(t, err)
		require.Len(t, confirmed, 1)
		assert.Equal(t, "retrievable-event", confirmed[0].EventID)
	})

	t.Run("stores event with pending status", func(t *testing.T) {
		database := newTestDB(t)
		pc := newTestPushCoreClient()
		logger := zerolog.Nop()

		el, err := NewEventListener(pc, database, logger, nil)
		require.NoError(t, err)

		event := &store.Event{
			EventID:          "pending-event",
			BlockHeight:      600,
			Type:             store.EventTypeSignOutbound,
			ConfirmationType: store.ConfirmationInstant,
			Status:           store.StatusPending,
			EventData:        []byte(`{}`),
		}

		result := el.storeEvent(event)
		assert.Equal(t, 1, result)

		cs := common.NewChainStore(database)
		pending, err := cs.GetPendingEvents(10)
		require.NoError(t, err)
		require.Len(t, pending, 1)
		assert.Equal(t, "pending-event", pending[0].EventID)
	})
}
