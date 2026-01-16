package push

import (
	"context"
	"testing"
	"time"

	"github.com/pushchain/push-chain-node/universalClient/config"
	"github.com/pushchain/push-chain-node/universalClient/db"
	"github.com/pushchain/push-chain-node/universalClient/pushcore"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestPushCoreClient creates a minimal pushcore.Client for testing
// This creates a client with empty slices, which will fail on actual operations
// but allows testing the structure
func newTestPushCoreClient() *pushcore.Client {
	return &pushcore.Client{
		// Empty slices - actual operations will fail but structure is valid
	}
}

func newTestDB(t *testing.T) *db.DB {
	testDB, err := db.OpenInMemoryDB(true)
	require.NoError(t, err)

	return testDB
}

func TestNewEventListener(t *testing.T) {
	logger := zerolog.Nop()
	db := newTestDB(t)
	client := newTestPushCoreClient()

	t.Run("success with nil chainConfig", func(t *testing.T) {
		listener, err := NewEventListener(client, db, logger, nil)
		require.NoError(t, err)
		require.NotNil(t, listener)

		// Verify defaults applied
		assert.Equal(t, DefaultPollInterval, listener.cfg.PollInterval)
		assert.Equal(t, uint64(DefaultChunkSize), listener.cfg.ChunkSize)
		assert.Equal(t, uint64(DefaultQueryLimit), listener.cfg.QueryLimit)
	})

	t.Run("success with chainConfig that sets poll interval", func(t *testing.T) {
		pollInterval := int(10)
		chainConfig := &config.ChainSpecificConfig{
			EventPollingIntervalSeconds: &pollInterval,
		}
		listener, err := NewEventListener(client, db, logger, chainConfig)
		require.NoError(t, err)
		require.NotNil(t, listener)

		assert.Equal(t, 10*time.Second, listener.cfg.PollInterval)
		assert.Equal(t, uint64(DefaultChunkSize), listener.cfg.ChunkSize)
		assert.Equal(t, uint64(DefaultQueryLimit), listener.cfg.QueryLimit)
	})

	t.Run("nil client error", func(t *testing.T) {
		listener, err := NewEventListener(nil, db, logger, nil)
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrNilClient)
		assert.Nil(t, listener)
	})

	t.Run("nil database error", func(t *testing.T) {
		listener, err := NewEventListener(client, nil, logger, nil)
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrNilDatabase)
		assert.Nil(t, listener)
	})

	t.Run("poll interval of 0 uses default", func(t *testing.T) {
		// When poll interval is 0, it should use the default (condition is > 0)
		pollInterval := int(0)
		chainConfig := &config.ChainSpecificConfig{
			EventPollingIntervalSeconds: &pollInterval,
		}
		listener, err := NewEventListener(client, db, logger, chainConfig)
		require.NoError(t, err)
		require.NotNil(t, listener)
		// Should use default poll interval
		assert.Equal(t, DefaultPollInterval, listener.cfg.PollInterval)
	})

	t.Run("invalid poll interval - too long", func(t *testing.T) {
		pollInterval := int(600) // More than maxPollInterval (5 minutes = 300 seconds)
		chainConfig := &config.ChainSpecificConfig{
			EventPollingIntervalSeconds: &pollInterval,
		}
		listener, err := NewEventListener(client, db, logger, chainConfig)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "poll interval")
		assert.Nil(t, listener)
	})
}

func TestListener_StartStop(t *testing.T) {
	logger := zerolog.Nop()
	db := newTestDB(t)
	client := newTestPushCoreClient()

	listener, err := NewEventListener(client, db, logger, nil)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	t.Run("start successfully", func(t *testing.T) {
		err := listener.Start(ctx)
		require.NoError(t, err)
		assert.True(t, listener.IsRunning())
	})

	t.Run("start when already running returns error", func(t *testing.T) {
		err := listener.Start(ctx)
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrAlreadyRunning)
	})

	t.Run("stop successfully", func(t *testing.T) {
		err := listener.Stop()
		require.NoError(t, err)
		assert.False(t, listener.IsRunning())
	})

	t.Run("stop when not running returns error", func(t *testing.T) {
		err := listener.Stop()
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrNotRunning)
	})
}

func TestListener_IsRunning(t *testing.T) {
	logger := zerolog.Nop()
	db := newTestDB(t)
	client := newTestPushCoreClient()

	listener, err := NewEventListener(client, db, logger, nil)
	require.NoError(t, err)

	assert.False(t, listener.IsRunning())

	ctx := context.Background()
	err = listener.Start(ctx)
	require.NoError(t, err)
	assert.True(t, listener.IsRunning())

	err = listener.Stop()
	require.NoError(t, err)
	assert.False(t, listener.IsRunning())
}

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{
			name:    "valid config",
			cfg:     Config{PollInterval: 5 * time.Second},
			wantErr: false,
		},
		{
			name:    "min poll interval",
			cfg:     Config{PollInterval: 1 * time.Second},
			wantErr: false,
		},
		{
			name:    "max poll interval",
			cfg:     Config{PollInterval: 5 * time.Minute},
			wantErr: false,
		},
		{
			name:    "too short poll interval",
			cfg:     Config{PollInterval: 500 * time.Millisecond},
			wantErr: true,
		},
		{
			name:    "too long poll interval",
			cfg:     Config{PollInterval: 10 * time.Minute},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestConfig_ApplyDefaults(t *testing.T) {
	t.Run("all zero values", func(t *testing.T) {
		cfg := &Config{}
		cfg.applyDefaults()

		assert.Equal(t, DefaultPollInterval, cfg.PollInterval)
		assert.Equal(t, uint64(DefaultChunkSize), cfg.ChunkSize)
		assert.Equal(t, uint64(DefaultQueryLimit), cfg.QueryLimit)
	})

	t.Run("partial values", func(t *testing.T) {
		cfg := &Config{
			PollInterval: 10 * time.Second,
		}
		cfg.applyDefaults()

		assert.Equal(t, 10*time.Second, cfg.PollInterval)
		assert.Equal(t, uint64(DefaultChunkSize), cfg.ChunkSize)
		assert.Equal(t, uint64(DefaultQueryLimit), cfg.QueryLimit)
	})

	t.Run("all values set", func(t *testing.T) {
		cfg := &Config{
			PollInterval: 10 * time.Second,
			ChunkSize:    500,
			QueryLimit:   50,
		}
		cfg.applyDefaults()

		assert.Equal(t, 10*time.Second, cfg.PollInterval)
		assert.Equal(t, uint64(500), cfg.ChunkSize)
		assert.Equal(t, uint64(50), cfg.QueryLimit)
	})
}
