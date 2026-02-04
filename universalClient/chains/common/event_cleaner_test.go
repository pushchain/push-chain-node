package common

import (
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewEventCleaner(t *testing.T) {
	t.Run("creates event cleaner with valid params", func(t *testing.T) {
		logger := zerolog.Nop()
		cleanupInterval := 1 * time.Hour
		retentionPeriod := 24 * time.Hour
		chainID := "eip155:1"

		cleaner := NewEventCleaner(nil, cleanupInterval, retentionPeriod, chainID, logger)

		require.NotNil(t, cleaner)
		assert.Equal(t, cleanupInterval, cleaner.cleanupInterval)
		assert.Equal(t, retentionPeriod, cleaner.retentionPeriod)
		assert.Nil(t, cleaner.database)
		assert.NotNil(t, cleaner.stopCh)
	})

	t.Run("creates event cleaner with different intervals", func(t *testing.T) {
		logger := zerolog.Nop()

		testCases := []struct {
			cleanup   time.Duration
			retention time.Duration
		}{
			{30 * time.Minute, 12 * time.Hour},
			{1 * time.Hour, 48 * time.Hour},
			{5 * time.Minute, 1 * time.Hour},
		}

		for _, tc := range testCases {
			cleaner := NewEventCleaner(nil, tc.cleanup, tc.retention, "test-chain", logger)
			assert.Equal(t, tc.cleanup, cleaner.cleanupInterval)
			assert.Equal(t, tc.retention, cleaner.retentionPeriod)
		}
	})
}

func TestEventCleanerStruct(t *testing.T) {
	t.Run("struct has expected fields", func(t *testing.T) {
		ec := &EventCleaner{}
		assert.Nil(t, ec.database)
		assert.Equal(t, time.Duration(0), ec.cleanupInterval)
		assert.Equal(t, time.Duration(0), ec.retentionPeriod)
		assert.Nil(t, ec.ticker)
		assert.Nil(t, ec.stopCh)
	})
}

func TestEventCleanerStop(t *testing.T) {
	t.Run("stop closes channel", func(t *testing.T) {
		logger := zerolog.Nop()
		cleaner := NewEventCleaner(nil, time.Hour, time.Hour, "test-chain", logger)

		// Start a ticker to test stop
		cleaner.ticker = time.NewTicker(time.Hour)

		// Should not panic
		cleaner.Stop()
	})

	t.Run("stop with nil ticker", func(t *testing.T) {
		logger := zerolog.Nop()
		cleaner := NewEventCleaner(nil, time.Hour, time.Hour, "test-chain", logger)
		cleaner.ticker = nil

		// Should not panic
		cleaner.Stop()
	})
}
