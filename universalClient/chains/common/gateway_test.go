package common

import (
	"math/big"
	"testing"

	"github.com/stretchr/testify/assert"

	uetypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
)

func TestUniversalTxStruct(t *testing.T) {
	t.Run("struct fields", func(t *testing.T) {
		tx := UniversalTx{
			SourceChain:         "eip155:1",
			LogIndex:            5,
			Sender:              "0x1234567890123456789012345678901234567890",
			Recipient:           "push1recipient123",
			Token:               "0xTokenAddress",
			Amount:              "1000000000000000000",
			Payload:             uetypes.UniversalPayload{},
			VerificationData:    "0xverification",
			RevertFundRecipient: "0xrevert",
			RevertMsg:           "0xrevertmsg",
			TxType:              2,
		}

		assert.Equal(t, "eip155:1", tx.SourceChain)
		assert.Equal(t, uint(5), tx.LogIndex)
		assert.Equal(t, "0x1234567890123456789012345678901234567890", tx.Sender)
		assert.Equal(t, "push1recipient123", tx.Recipient)
		assert.Equal(t, "0xTokenAddress", tx.Token)
		assert.Equal(t, "1000000000000000000", tx.Amount)
		assert.Equal(t, "0xverification", tx.VerificationData)
		assert.Equal(t, "0xrevert", tx.RevertFundRecipient)
		assert.Equal(t, "0xrevertmsg", tx.RevertMsg)
		assert.Equal(t, uint(2), tx.TxType)
	})

	t.Run("empty struct", func(t *testing.T) {
		tx := UniversalTx{}
		assert.Empty(t, tx.SourceChain)
		assert.Equal(t, uint(0), tx.LogIndex)
		assert.Empty(t, tx.Sender)
		assert.Empty(t, tx.Amount)
	})
}

func TestOutboundEventStruct(t *testing.T) {
	t.Run("struct fields", func(t *testing.T) {
		event := OutboundEvent{
			TxID:          "0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
			UniversalTxID: "0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
		}

		assert.Equal(t, "0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef", event.TxID)
		assert.Equal(t, "0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890", event.UniversalTxID)
	})

	t.Run("empty struct", func(t *testing.T) {
		event := OutboundEvent{}
		assert.Empty(t, event.TxID)
		assert.Empty(t, event.UniversalTxID)
	})
}

func TestOutboundTxResultStruct(t *testing.T) {
	t.Run("struct fields", func(t *testing.T) {
		result := OutboundTxResult{
			SigningHash: []byte{0x01, 0x02, 0x03},
			Nonce:       42,
			GasPrice:    big.NewInt(20000000000),
			GasLimit:    21000,
			ChainID:     "eip155:1",
			RawTx:       []byte{0x04, 0x05, 0x06},
		}

		assert.Equal(t, []byte{0x01, 0x02, 0x03}, result.SigningHash)
		assert.Equal(t, uint64(42), result.Nonce)
		assert.Equal(t, big.NewInt(20000000000), result.GasPrice)
		assert.Equal(t, uint64(21000), result.GasLimit)
		assert.Equal(t, "eip155:1", result.ChainID)
		assert.Equal(t, []byte{0x04, 0x05, 0x06}, result.RawTx)
	})

	t.Run("empty struct", func(t *testing.T) {
		result := OutboundTxResult{}
		assert.Nil(t, result.SigningHash)
		assert.Equal(t, uint64(0), result.Nonce)
		assert.Nil(t, result.GasPrice)
		assert.Equal(t, uint64(0), result.GasLimit)
		assert.Empty(t, result.ChainID)
		assert.Nil(t, result.RawTx)
	})

	t.Run("gas price operations", func(t *testing.T) {
		result := OutboundTxResult{
			GasPrice: big.NewInt(1000000000),
		}

		// Test big.Int operations
		assert.Equal(t, int64(1000000000), result.GasPrice.Int64())
		assert.Equal(t, "1000000000", result.GasPrice.String())
	})
}

func TestTxTypes(t *testing.T) {
	t.Run("tx type values", func(t *testing.T) {
		// Test that tx types can be stored as uint
		txTypeGas := uint(0)
		txTypeGasAndPayload := uint(1)
		txTypeFunds := uint(2)
		txTypeFundsAndPayload := uint(3)

		assert.Equal(t, uint(0), txTypeGas)
		assert.Equal(t, uint(1), txTypeGasAndPayload)
		assert.Equal(t, uint(2), txTypeFunds)
		assert.Equal(t, uint(3), txTypeFundsAndPayload)
	})
}
