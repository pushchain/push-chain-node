package push

import (
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigValidate(t *testing.T) {
	t.Run("valid config passes validation", func(t *testing.T) {
		cfg := &Config{
			PollInterval: 5 * time.Second,
			ChunkSize:    1000,
			QueryLimit:   100,
		}
		err := cfg.Validate()
		assert.NoError(t, err)
	})

	t.Run("poll interval too short", func(t *testing.T) {
		cfg := &Config{
			PollInterval: 500 * time.Millisecond,
		}
		err := cfg.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "too short")
	})

	t.Run("poll interval too long", func(t *testing.T) {
		cfg := &Config{
			PollInterval: 10 * time.Minute,
		}
		err := cfg.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "too long")
	})

	t.Run("minimum valid poll interval", func(t *testing.T) {
		cfg := &Config{
			PollInterval: minPollInterval,
		}
		err := cfg.Validate()
		assert.NoError(t, err)
	})

	t.Run("maximum valid poll interval", func(t *testing.T) {
		cfg := &Config{
			PollInterval: maxPollInterval,
		}
		err := cfg.Validate()
		assert.NoError(t, err)
	})
}

func TestConfigApplyDefaults(t *testing.T) {
	t.Run("applies defaults to zero values", func(t *testing.T) {
		cfg := &Config{}
		cfg.applyDefaults()

		assert.Equal(t, DefaultPollInterval, cfg.PollInterval)
		assert.Equal(t, uint64(DefaultChunkSize), cfg.ChunkSize)
		assert.Equal(t, uint64(DefaultQueryLimit), cfg.QueryLimit)
	})

	t.Run("does not override non-zero values", func(t *testing.T) {
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

	t.Run("partially applies defaults", func(t *testing.T) {
		cfg := &Config{
			PollInterval: 10 * time.Second,
		}
		cfg.applyDefaults()

		assert.Equal(t, 10*time.Second, cfg.PollInterval)
		assert.Equal(t, uint64(DefaultChunkSize), cfg.ChunkSize)
		assert.Equal(t, uint64(DefaultQueryLimit), cfg.QueryLimit)
	})
}

func TestDefaultConstants(t *testing.T) {
	t.Run("default poll interval", func(t *testing.T) {
		assert.Equal(t, 5*time.Second, DefaultPollInterval)
	})

	t.Run("default chunk size", func(t *testing.T) {
		assert.Equal(t, 1000, DefaultChunkSize)
	})

	t.Run("default query limit", func(t *testing.T) {
		assert.Equal(t, 100, DefaultQueryLimit)
	})

	t.Run("min poll interval", func(t *testing.T) {
		assert.Equal(t, 1*time.Second, minPollInterval)
	})

	t.Run("max poll interval", func(t *testing.T) {
		assert.Equal(t, 5*time.Minute, maxPollInterval)
	})
}

func TestEventQueries(t *testing.T) {
	t.Run("TSS event query format", func(t *testing.T) {
		assert.Contains(t, TSSEventQuery, EventTypeTSSProcessInitiated)
		assert.Contains(t, TSSEventQuery, "process_id>=0")
	})

	t.Run("outbound event query format", func(t *testing.T) {
		assert.Contains(t, OutboundEventQuery, EventTypeOutboundCreated)
		assert.Contains(t, OutboundEventQuery, "tx_id EXISTS")
	})
}

func TestErrors(t *testing.T) {
	t.Run("ErrNilClient message", func(t *testing.T) {
		assert.Equal(t, "push client is nil", ErrNilClient.Error())
	})

	t.Run("ErrNilDatabase message", func(t *testing.T) {
		assert.Equal(t, "database is nil", ErrNilDatabase.Error())
	})

	t.Run("ErrAlreadyRunning message", func(t *testing.T) {
		assert.Equal(t, "event listener is already running", ErrAlreadyRunning.Error())
	})

	t.Run("ErrNotRunning message", func(t *testing.T) {
		assert.Equal(t, "event listener is not running", ErrNotRunning.Error())
	})
}

func TestNewEventListenerErrors(t *testing.T) {
	t.Run("nil client returns error", func(t *testing.T) {
		listener, err := NewEventListener(nil, nil, zerolog.Nop(), nil)
		require.Error(t, err)
		assert.Nil(t, listener)
		assert.Equal(t, ErrNilClient, err)
	})
}

func TestMin(t *testing.T) {
	testCases := []struct {
		a, b     uint64
		expected uint64
	}{
		{1, 2, 1},
		{2, 1, 1},
		{5, 5, 5},
		{0, 10, 0},
		{100, 50, 50},
		{1000000, 999999, 999999},
	}

	for _, tc := range testCases {
		result := min(tc.a, tc.b)
		assert.Equal(t, tc.expected, result, "min(%d, %d) should be %d", tc.a, tc.b, tc.expected)
	}
}

func TestEventListenerIsRunning(t *testing.T) {
	t.Run("returns false when not running", func(t *testing.T) {
		el := &EventListener{running: false}
		assert.False(t, el.IsRunning())
	})

	t.Run("returns true when running", func(t *testing.T) {
		el := &EventListener{running: true}
		assert.True(t, el.IsRunning())
	})
}

func TestEventListenerStop(t *testing.T) {
	t.Run("stop when not running returns error", func(t *testing.T) {
		el := &EventListener{running: false}
		err := el.Stop()
		require.Error(t, err)
		assert.Equal(t, ErrNotRunning, err)
	})
}

func TestEventListenerStruct(t *testing.T) {
	t.Run("struct has expected fields", func(t *testing.T) {
		el := &EventListener{}
		assert.Nil(t, el.pushCore)
		assert.Nil(t, el.database)
		assert.Nil(t, el.chainStore)
		assert.Nil(t, el.chainConfig)
		assert.Nil(t, el.stopCh)
		assert.False(t, el.running)
	})
}
