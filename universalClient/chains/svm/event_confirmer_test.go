package svm

import (
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewEventConfirmer(t *testing.T) {
	t.Run("creates event confirmer with valid params", func(t *testing.T) {
		logger := zerolog.Nop()
		chainID := "solana:mainnet"

		confirmer := NewEventConfirmer(nil, nil, chainID, 5, 5, 12, logger)

		require.NotNil(t, confirmer)
		assert.Equal(t, chainID, confirmer.chainID)
		assert.Equal(t, 5, confirmer.pollIntervalSeconds)
		assert.Equal(t, uint64(5), confirmer.fastConfirmations)
		assert.Equal(t, uint64(12), confirmer.standardConfirmations)
		assert.NotNil(t, confirmer.chainStore)
		assert.NotNil(t, confirmer.stopCh)
	})

	t.Run("creates event confirmer with different confirmation counts", func(t *testing.T) {
		logger := zerolog.Nop()

		testCases := []struct {
			fast     uint64
			standard uint64
		}{
			{1, 6},
			{5, 12},
			{10, 20},
			{0, 1},
		}

		for _, tc := range testCases {
			confirmer := NewEventConfirmer(nil, nil, "solana:mainnet", 5, tc.fast, tc.standard, logger)
			assert.Equal(t, tc.fast, confirmer.fastConfirmations)
			assert.Equal(t, tc.standard, confirmer.standardConfirmations)
		}
	})
}

func TestEventConfirmerGetTxSignatureFromEventID(t *testing.T) {
	logger := zerolog.Nop()
	confirmer := NewEventConfirmer(nil, nil, "solana:mainnet", 5, 5, 12, logger)

	t.Run("extracts signature from standard format", func(t *testing.T) {
		eventID := "5VERv8NMvzbJMEkV8xnrLkEaWRtSz9CosKDYjCJjBRnbJLgp8uirBgmQpjKhoR4tjF3ZpRzrFmBV6UjKdiSZkQUW:0"
		sig := confirmer.getTxSignatureFromEventID(eventID)
		assert.Equal(t, "5VERv8NMvzbJMEkV8xnrLkEaWRtSz9CosKDYjCJjBRnbJLgp8uirBgmQpjKhoR4tjF3ZpRzrFmBV6UjKdiSZkQUW", sig)
	})

	t.Run("extracts signature with log index", func(t *testing.T) {
		eventID := "abc123def456:5"
		sig := confirmer.getTxSignatureFromEventID(eventID)
		assert.Equal(t, "abc123def456", sig)
	})

	t.Run("handles event ID without colon", func(t *testing.T) {
		eventID := "abc123def456"
		sig := confirmer.getTxSignatureFromEventID(eventID)
		assert.Equal(t, "abc123def456", sig)
	})

	t.Run("returns empty string for empty event ID", func(t *testing.T) {
		eventID := ""
		sig := confirmer.getTxSignatureFromEventID(eventID)
		assert.Empty(t, sig)
	})

	t.Run("handles multiple colons", func(t *testing.T) {
		eventID := "sig:123:456:789"
		sig := confirmer.getTxSignatureFromEventID(eventID)
		assert.Equal(t, "sig", sig)
	})
}

func TestEventConfirmerGetRequiredConfirmations(t *testing.T) {
	logger := zerolog.Nop()

	t.Run("FAST confirmation type with custom value", func(t *testing.T) {
		confirmer := NewEventConfirmer(nil, nil, "solana:mainnet", 5, 5, 12, logger)
		confirmations := confirmer.getRequiredConfirmations("FAST")
		assert.Equal(t, uint64(5), confirmations)
	})

	t.Run("FAST confirmation type with zero uses default", func(t *testing.T) {
		confirmer := NewEventConfirmer(nil, nil, "solana:mainnet", 5, 0, 12, logger)
		confirmations := confirmer.getRequiredConfirmations("FAST")
		assert.Equal(t, uint64(5), confirmations) // Default is 5
	})

	t.Run("STANDARD confirmation type with custom value", func(t *testing.T) {
		confirmer := NewEventConfirmer(nil, nil, "solana:mainnet", 5, 5, 20, logger)
		confirmations := confirmer.getRequiredConfirmations("STANDARD")
		assert.Equal(t, uint64(20), confirmations)
	})

	t.Run("STANDARD confirmation type with zero uses default", func(t *testing.T) {
		confirmer := NewEventConfirmer(nil, nil, "solana:mainnet", 5, 5, 0, logger)
		confirmations := confirmer.getRequiredConfirmations("STANDARD")
		assert.Equal(t, uint64(12), confirmations) // Default is 12
	})

	t.Run("unknown type defaults to standard", func(t *testing.T) {
		confirmer := NewEventConfirmer(nil, nil, "solana:mainnet", 5, 5, 15, logger)
		confirmations := confirmer.getRequiredConfirmations("UNKNOWN")
		assert.Equal(t, uint64(15), confirmations)
	})

	t.Run("empty type defaults to standard", func(t *testing.T) {
		confirmer := NewEventConfirmer(nil, nil, "solana:mainnet", 5, 5, 15, logger)
		confirmations := confirmer.getRequiredConfirmations("")
		assert.Equal(t, uint64(15), confirmations)
	})
}

func TestEventConfirmerStop(t *testing.T) {
	t.Run("stop waits for goroutine", func(t *testing.T) {
		logger := zerolog.Nop()
		confirmer := NewEventConfirmer(nil, nil, "solana:mainnet", 5, 5, 12, logger)

		// Should not panic or hang
		confirmer.Stop()
	})
}

func TestEventConfirmerStruct(t *testing.T) {
	t.Run("struct has expected fields", func(t *testing.T) {
		ec := &EventConfirmer{}
		assert.Nil(t, ec.rpcClient)
		assert.Nil(t, ec.chainStore)
		assert.Empty(t, ec.chainID)
		assert.Equal(t, 0, ec.pollIntervalSeconds)
		assert.Equal(t, uint64(0), ec.fastConfirmations)
		assert.Equal(t, uint64(0), ec.standardConfirmations)
		assert.Nil(t, ec.stopCh)
	})
}
