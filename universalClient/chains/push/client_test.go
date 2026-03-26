package push

import (
	"context"
	"testing"

	"github.com/pushchain/push-chain-node/universalClient/config"
	"github.com/pushchain/push-chain-node/universalClient/db"
	"github.com/pushchain/push-chain-node/universalClient/pushcore"
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
