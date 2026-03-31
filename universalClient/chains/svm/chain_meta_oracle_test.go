package svm

import (
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewChainMetaOracle(t *testing.T) {
	t.Run("creates gas oracle with valid params", func(t *testing.T) {
		logger := zerolog.Nop()
		chainID := "solana:mainnet"
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
		oracle := NewChainMetaOracle(nil, nil, "solana:mainnet", 30, 15, logger)

		require.NotNil(t, oracle)
		assert.Equal(t, 15, oracle.gasPriceMarkupPercent)
	})

	t.Run("creates gas oracle with zero markup percent", func(t *testing.T) {
		logger := zerolog.Nop()
		oracle := NewChainMetaOracle(nil, nil, "solana:mainnet", 30, 0, logger)

		require.NotNil(t, oracle)
		assert.Equal(t, 0, oracle.gasPriceMarkupPercent)
	})

	t.Run("creates gas oracle with different chain IDs", func(t *testing.T) {
		logger := zerolog.Nop()

		testCases := []string{
			"solana:mainnet",
			"solana:devnet",
			"solana:testnet",
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
		oracle := NewChainMetaOracle(nil, nil, "solana:mainnet", 60, 0, logger)
		interval := oracle.getChainMetaOracleFetchInterval()
		assert.Equal(t, 60*time.Second, interval)
	})

	t.Run("returns default for zero interval", func(t *testing.T) {
		oracle := NewChainMetaOracle(nil, nil, "solana:mainnet", 0, 0, logger)
		interval := oracle.getChainMetaOracleFetchInterval()
		assert.Equal(t, 30*time.Second, interval)
	})

	t.Run("returns default for negative interval", func(t *testing.T) {
		oracle := NewChainMetaOracle(nil, nil, "solana:mainnet", -10, 0, logger)
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
			oracle := NewChainMetaOracle(nil, nil, "solana:mainnet", tc.input, 0, logger)
			interval := oracle.getChainMetaOracleFetchInterval()
			assert.Equal(t, tc.expected, interval, "interval %d should result in %v", tc.input, tc.expected)
		}
	})
}

func TestChainMetaOracleStop(t *testing.T) {
	t.Run("stop waits for goroutine", func(t *testing.T) {
		logger := zerolog.Nop()
		oracle := NewChainMetaOracle(nil, nil, "solana:mainnet", 30, 0, logger)

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
