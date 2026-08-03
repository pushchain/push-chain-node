package evm

import (
	"context"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewChainMetaOracle(t *testing.T) {
	t.Run("creates gas oracle with valid params", func(t *testing.T) {
		logger := zerolog.Nop()
		chainID := "eip155:1"
		interval := 30

		oracle := NewChainMetaOracle(nil, nil, chainID, interval, 0, logger)

		require.NotNil(t, oracle)
		assert.Equal(t, chainID, oracle.chainID)
		assert.Equal(t, interval, oracle.gasPriceIntervalSeconds)
		assert.Equal(t, 0, oracle.gasPriceMarkupPercent)
		assert.Nil(t, oracle.rpcClient)
		assert.Nil(t, oracle.pushSigner)
		assert.NotNil(t, oracle.stopCh)
	})

	t.Run("creates gas oracle with markup percent", func(t *testing.T) {
		logger := zerolog.Nop()
		oracle := NewChainMetaOracle(nil, nil, "eip155:42161", 30, 10, logger)

		require.NotNil(t, oracle)
		assert.Equal(t, 10, oracle.gasPriceMarkupPercent)
	})

	t.Run("creates gas oracle with zero markup percent", func(t *testing.T) {
		logger := zerolog.Nop()
		oracle := NewChainMetaOracle(nil, nil, "eip155:1", 30, 0, logger)

		require.NotNil(t, oracle)
		assert.Equal(t, 0, oracle.gasPriceMarkupPercent)
	})

	t.Run("creates gas oracle with different chain IDs", func(t *testing.T) {
		logger := zerolog.Nop()

		testCases := []string{
			"eip155:1",
			"eip155:97",
			"eip155:137",
			"eip155:42161",
		}

		for _, chainID := range testCases {
			oracle := NewChainMetaOracle(nil, nil, chainID, 30, 0, logger)
			assert.Equal(t, chainID, oracle.chainID)
		}
	})
}

func TestChainMetaOracleGetChainMetaOracleFetchInterval(t *testing.T) {
	logger := zerolog.Nop()

	t.Run("returns configured interval", func(t *testing.T) {
		oracle := NewChainMetaOracle(nil, nil, "eip155:1", 60, 0, logger)
		interval := oracle.getChainMetaOracleFetchInterval()
		assert.Equal(t, 60*time.Second, interval)
	})

	t.Run("returns default for zero interval", func(t *testing.T) {
		oracle := NewChainMetaOracle(nil, nil, "eip155:1", 0, 0, logger)
		interval := oracle.getChainMetaOracleFetchInterval()
		assert.Equal(t, 30*time.Second, interval)
	})

	t.Run("returns default for negative interval", func(t *testing.T) {
		oracle := NewChainMetaOracle(nil, nil, "eip155:1", -10, 0, logger)
		interval := oracle.getChainMetaOracleFetchInterval()
		assert.Equal(t, 30*time.Second, interval)
	})

	t.Run("respects custom intervals", func(t *testing.T) {
		testCases := []struct {
			input    int
			expected time.Duration
		}{
			{10, 10 * time.Second},
			{30, 30 * time.Second},
			{60, 60 * time.Second},
			{120, 120 * time.Second},
		}

		for _, tc := range testCases {
			oracle := NewChainMetaOracle(nil, nil, "eip155:1", tc.input, 0, logger)
			interval := oracle.getChainMetaOracleFetchInterval()
			assert.Equal(t, tc.expected, interval, "interval %d should result in %v", tc.input, tc.expected)
		}
	})
}

func TestChainMetaOracleStop(t *testing.T) {
	t.Run("stop waits for goroutine", func(t *testing.T) {
		logger := zerolog.Nop()
		oracle := NewChainMetaOracle(nil, nil, "eip155:1", 30, 0, logger)

		// Should not panic or hang
		oracle.Stop()
	})
}

func TestChainMetaOracleStruct(t *testing.T) {
	t.Run("struct has expected fields", func(t *testing.T) {
		oracle := &ChainMetaOracle{}
		assert.Nil(t, oracle.rpcClient)
		assert.Nil(t, oracle.pushSigner)
		assert.Empty(t, oracle.chainID)
		assert.Equal(t, 0, oracle.gasPriceIntervalSeconds)
		assert.Equal(t, 0, oracle.gasPriceMarkupPercent)
		assert.Nil(t, oracle.stopCh)
	})
}

func TestChainMetaOracleStartStop_ContextCancel(t *testing.T) {
	logger := zerolog.Nop()
	oracle := NewChainMetaOracle(nil, nil, "eip155:1", 30, 0, logger)

	ctx, cancel := context.WithCancel(context.Background())
	err := oracle.Start(ctx)
	require.NoError(t, err)

	// Let the goroutine spin up.
	time.Sleep(50 * time.Millisecond)

	cancel()
	// Stop should return promptly after context cancel.
	done := make(chan struct{})
	go func() {
		oracle.Stop()
		close(done)
	}()

	select {
	case <-done:
		// success
	case <-time.After(2 * time.Second):
		t.Fatal("Stop did not return within 2 seconds after context cancel")
	}
}

func TestChainMetaOracleStartStop_ViaStopCh(t *testing.T) {
	logger := zerolog.Nop()
	oracle := NewChainMetaOracle(nil, nil, "eip155:1", 30, 0, logger)

	ctx := context.Background()
	err := oracle.Start(ctx)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	done := make(chan struct{})
	go func() {
		oracle.Stop()
		close(done)
	}()

	select {
	case <-done:
		// success
	case <-time.After(2 * time.Second):
		t.Fatal("Stop did not return within 2 seconds")
	}
}

func TestChainMetaOracle_StartReturnsNilError(t *testing.T) {
	logger := zerolog.Nop()
	oracle := NewChainMetaOracle(nil, nil, "eip155:1", 30, 0, logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := oracle.Start(ctx)
	assert.NoError(t, err)

	cancel()
	oracle.Stop()
}

func TestChainMetaOracle_NilPushSignerField(t *testing.T) {
	// Verify that an oracle created without a pushSigner stores nil.
	logger := zerolog.Nop()
	oracle := NewChainMetaOracle(nil, nil, "eip155:1", 30, 10, logger)
	assert.Nil(t, oracle.pushSigner)
	assert.Nil(t, oracle.rpcClient)
}

func TestGetChainMetaOracleFetchInterval_EdgeCases(t *testing.T) {
	logger := zerolog.Nop()

	t.Run("very large interval", func(t *testing.T) {
		oracle := NewChainMetaOracle(nil, nil, "eip155:1", 3600, 0, logger)
		interval := oracle.getChainMetaOracleFetchInterval()
		assert.Equal(t, 3600*time.Second, interval)
	})

	t.Run("interval of 1 second", func(t *testing.T) {
		oracle := NewChainMetaOracle(nil, nil, "eip155:1", 1, 0, logger)
		interval := oracle.getChainMetaOracleFetchInterval()
		assert.Equal(t, 1*time.Second, interval)
	})

	t.Run("very large negative interval defaults to 30s", func(t *testing.T) {
		oracle := NewChainMetaOracle(nil, nil, "eip155:1", -9999, 0, logger)
		interval := oracle.getChainMetaOracleFetchInterval()
		assert.Equal(t, 30*time.Second, interval)
	})

	t.Run("fetchAndVoteChainMeta uses default for zero interval", func(t *testing.T) {
		// The function inside fetchAndVoteChainMeta has its own fallback check
		// (interval <= 0 -> 30s). We verify getChainMetaOracleFetchInterval
		// returns 30s which triggers that path.
		oracle := NewChainMetaOracle(nil, nil, "eip155:1", 0, 0, logger)
		interval := oracle.getChainMetaOracleFetchInterval()
		assert.Equal(t, 30*time.Second, interval)
	})
}

func TestChainMetaOracle_MarkupPercentValues(t *testing.T) {
	logger := zerolog.Nop()

	t.Run("negative markup percent stored as-is", func(t *testing.T) {
		oracle := NewChainMetaOracle(nil, nil, "eip155:1", 30, -5, logger)
		assert.Equal(t, -5, oracle.gasPriceMarkupPercent)
	})

	t.Run("high markup percent stored as-is", func(t *testing.T) {
		oracle := NewChainMetaOracle(nil, nil, "eip155:1", 30, 200, logger)
		assert.Equal(t, 200, oracle.gasPriceMarkupPercent)
	})
}
