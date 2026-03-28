package push

import (
	"context"
	"testing"
	"time"

	"github.com/pushchain/push-chain-node/universalClient/config"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewEventListener(t *testing.T) {
	logger := zerolog.Nop()
	db := newTestDB(t)
	client := newTestPushCoreClient()

	t.Run("success with defaults", func(t *testing.T) {
		el, err := NewEventListener(client, db, logger, nil)
		require.NoError(t, err)
		require.NotNil(t, el)
		assert.Equal(t, DefaultPollInterval, el.cfg.PollInterval)
		assert.False(t, el.IsRunning())
	})

	t.Run("nil client", func(t *testing.T) {
		_, err := NewEventListener(nil, db, logger, nil)
		assert.ErrorIs(t, err, ErrNilClient)
	})

	t.Run("nil database", func(t *testing.T) {
		_, err := NewEventListener(client, nil, logger, nil)
		assert.ErrorIs(t, err, ErrNilDatabase)
	})

	t.Run("custom poll interval from config", func(t *testing.T) {
		poll := 10
		cfg := config.ChainSpecificConfig{EventPollingIntervalSeconds: &poll}
		el, err := NewEventListener(client, db, logger, &cfg)
		require.NoError(t, err)
		assert.Equal(t, 10*time.Second, el.cfg.PollInterval)
	})

	t.Run("zero poll interval uses default", func(t *testing.T) {
		poll := 0
		cfg := config.ChainSpecificConfig{EventPollingIntervalSeconds: &poll}
		el, err := NewEventListener(client, db, logger, &cfg)
		require.NoError(t, err)
		assert.Equal(t, DefaultPollInterval, el.cfg.PollInterval)
	})
}

func TestEventListener_StartStop(t *testing.T) {
	el, err := NewEventListener(newTestPushCoreClient(), newTestDB(t), zerolog.Nop(), nil)
	require.NoError(t, err)

	ctx := context.Background()

	// Start
	require.NoError(t, el.Start(ctx))
	assert.True(t, el.IsRunning())

	// Double start
	assert.ErrorIs(t, el.Start(ctx), ErrAlreadyRunning)

	// Stop
	require.NoError(t, el.Stop())
	assert.False(t, el.IsRunning())

	// Double stop
	assert.ErrorIs(t, el.Stop(), ErrNotRunning)
}

func TestEventListener_RestartAfterStop(t *testing.T) {
	el, err := NewEventListener(newTestPushCoreClient(), newTestDB(t), zerolog.Nop(), nil)
	require.NoError(t, err)

	ctx := context.Background()

	require.NoError(t, el.Start(ctx))
	require.NoError(t, el.Stop())

	require.NoError(t, el.Start(ctx))
	require.NoError(t, el.Stop())
}

func TestErrors(t *testing.T) {
	assert.Equal(t, "push client is nil", ErrNilClient.Error())
	assert.Equal(t, "database is nil", ErrNilDatabase.Error())
	assert.Equal(t, "event listener is already running", ErrAlreadyRunning.Error())
	assert.Equal(t, "event listener is not running", ErrNotRunning.Error())
}

func TestDefaultPollInterval(t *testing.T) {
	assert.Equal(t, 2*time.Second, DefaultPollInterval)
}
