package evm

import (
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewEventConfirmer(t *testing.T) {
	t.Run("creates event confirmer with valid params", func(t *testing.T) {
		logger := zerolog.Nop()
		chainID := "eip155:1"

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
			confirmer := NewEventConfirmer(nil, nil, "eip155:1", 5, tc.fast, tc.standard, logger)
			assert.Equal(t, tc.fast, confirmer.fastConfirmations)
			assert.Equal(t, tc.standard, confirmer.standardConfirmations)
		}
	})
}

func TestEventConfirmerGetTxHashFromEventID(t *testing.T) {
	logger := zerolog.Nop()
	confirmer := NewEventConfirmer(nil, nil, "eip155:1", 5, 5, 12, logger)

	t.Run("extracts tx hash from standard format", func(t *testing.T) {
		eventID := "0x1234567890abcdef:5"
		txHash := confirmer.getTxHashFromEventID(eventID)
		assert.Equal(t, "0x1234567890abcdef", txHash)
	})

	t.Run("extracts tx hash with log index 0", func(t *testing.T) {
		eventID := "0xabc123:0"
		txHash := confirmer.getTxHashFromEventID(eventID)
		assert.Equal(t, "0xabc123", txHash)
	})

	t.Run("handles event ID without colon", func(t *testing.T) {
		eventID := "0x1234567890abcdef"
		txHash := confirmer.getTxHashFromEventID(eventID)
		assert.Equal(t, "0x1234567890abcdef", txHash)
	})

	t.Run("returns empty string for empty event ID", func(t *testing.T) {
		eventID := ""
		txHash := confirmer.getTxHashFromEventID(eventID)
		assert.Empty(t, txHash)
	})

	t.Run("handles multiple colons", func(t *testing.T) {
		eventID := "0x123:456:789"
		txHash := confirmer.getTxHashFromEventID(eventID)
		assert.Equal(t, "0x123", txHash)
	})
}

func TestEventConfirmerGetRequiredConfirmations(t *testing.T) {
	logger := zerolog.Nop()

	t.Run("FAST confirmation type", func(t *testing.T) {
		confirmer := NewEventConfirmer(nil, nil, "eip155:1", 5, 5, 12, logger)
		confirmations := confirmer.getRequiredConfirmations("FAST")
		assert.Equal(t, uint64(5), confirmations)
	})

	t.Run("STANDARD confirmation type", func(t *testing.T) {
		confirmer := NewEventConfirmer(nil, nil, "eip155:1", 5, 5, 12, logger)
		confirmations := confirmer.getRequiredConfirmations("STANDARD")
		assert.Equal(t, uint64(12), confirmations)
	})

	t.Run("unknown type defaults to standard", func(t *testing.T) {
		confirmer := NewEventConfirmer(nil, nil, "eip155:1", 5, 5, 12, logger)
		confirmations := confirmer.getRequiredConfirmations("UNKNOWN")
		assert.Equal(t, uint64(12), confirmations)
	})

	t.Run("empty type defaults to standard", func(t *testing.T) {
		confirmer := NewEventConfirmer(nil, nil, "eip155:1", 5, 5, 12, logger)
		confirmations := confirmer.getRequiredConfirmations("")
		assert.Equal(t, uint64(12), confirmations)
	})

	t.Run("uses custom fast confirmations", func(t *testing.T) {
		confirmer := NewEventConfirmer(nil, nil, "eip155:1", 5, 3, 20, logger)
		confirmations := confirmer.getRequiredConfirmations("FAST")
		assert.Equal(t, uint64(3), confirmations)
	})

	t.Run("uses custom standard confirmations", func(t *testing.T) {
		confirmer := NewEventConfirmer(nil, nil, "eip155:1", 5, 3, 20, logger)
		confirmations := confirmer.getRequiredConfirmations("STANDARD")
		assert.Equal(t, uint64(20), confirmations)
	})
}

func TestEventConfirmerStop(t *testing.T) {
	t.Run("stop waits for goroutine", func(t *testing.T) {
		logger := zerolog.Nop()
		confirmer := NewEventConfirmer(nil, nil, "eip155:1", 5, 5, 12, logger)

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
