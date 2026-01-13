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

// mockPushClient implements PushClient for testing.
type mockPushClient struct {
	latestBlock uint64
	txResults   []*pushcore.TxResult
	err         error
}

func (m *mockPushClient) GetLatestBlock() (uint64, error) {
	return m.latestBlock, m.err
}

func (m *mockPushClient) GetTxsByEvents(query string, minHeight, maxHeight uint64, limit uint64) ([]*pushcore.TxResult, error) {
	return m.txResults, m.err
}

func newTestDB(t *testing.T) *gorm.DB {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	require.NoError(t, err)

	// Use the actual store types for migration
	err = db.AutoMigrate(&store.ChainState{}, &store.PCEvent{})
	require.NoError(t, err)

	return db
}

func TestNewListener(t *testing.T) {
	logger := zerolog.Nop()
	db := newTestDB(t)
	client := &mockPushClient{}

	t.Run("success with nil config", func(t *testing.T) {
		listener, err := NewListener(client, db, logger, nil)
		require.NoError(t, err)
		require.NotNil(t, listener)

		// Verify defaults applied
		assert.Equal(t, DefaultPollInterval, listener.cfg.PollInterval)
		assert.Equal(t, uint64(DefaultChunkSize), listener.cfg.ChunkSize)
		assert.Equal(t, uint64(DefaultQueryLimit), listener.cfg.QueryLimit)
	})

	t.Run("success with custom config", func(t *testing.T) {
		cfg := &Config{
			PollInterval: 10 * time.Second,
			ChunkSize:    500,
			QueryLimit:   50,
		}
		listener, err := NewListener(client, db, logger, cfg)
		require.NoError(t, err)
		require.NotNil(t, listener)

		assert.Equal(t, 10*time.Second, listener.cfg.PollInterval)
		assert.Equal(t, uint64(500), listener.cfg.ChunkSize)
		assert.Equal(t, uint64(50), listener.cfg.QueryLimit)
	})

	t.Run("nil client error", func(t *testing.T) {
		listener, err := NewListener(nil, db, logger, nil)
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrNilClient)
		assert.Nil(t, listener)
	})

	t.Run("nil database error", func(t *testing.T) {
		listener, err := NewListener(client, nil, logger, nil)
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrNilDatabase)
		assert.Nil(t, listener)
	})

	t.Run("invalid poll interval - too short", func(t *testing.T) {
		cfg := &Config{
			PollInterval: 100 * time.Millisecond, // Less than minPollInterval
		}
		listener, err := NewListener(client, db, logger, cfg)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "poll interval")
		assert.Nil(t, listener)
	})

	t.Run("invalid poll interval - too long", func(t *testing.T) {
		cfg := &Config{
			PollInterval: 10 * time.Minute, // More than maxPollInterval
		}
		listener, err := NewListener(client, db, logger, cfg)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "poll interval")
		assert.Nil(t, listener)
	})
}

func TestListener_StartStop(t *testing.T) {
	logger := zerolog.Nop()
	db := newTestDB(t)
	client := &mockPushClient{latestBlock: 100}

	listener, err := NewListener(client, db, logger, nil)
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
	client := &mockPushClient{latestBlock: 100}

	listener, err := NewListener(client, db, logger, nil)
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
