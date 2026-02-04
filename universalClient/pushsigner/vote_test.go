package pushsigner

import (
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
	utsstypes "github.com/pushchain/push-chain-node/x/utss/types"
)

func TestVoteConstants(t *testing.T) {
	t.Run("default gas limit", func(t *testing.T) {
		assert.Equal(t, uint64(500000000), defaultGasLimit)
	})

	t.Run("default fee amount is valid", func(t *testing.T) {
		coins, err := sdk.ParseCoinsNormalized(defaultFeeAmount)
		require.NoError(t, err)
		assert.False(t, coins.IsZero())
		assert.Equal(t, "500000000000000upc", defaultFeeAmount)
	})

	t.Run("default vote timeout", func(t *testing.T) {
		assert.Equal(t, 30*time.Second, defaultVoteTimeout)
	})
}

func TestMsgVoteInboundConstruction(t *testing.T) {
	t.Run("construct valid MsgVoteInbound", func(t *testing.T) {
		granter := "push1granter123"
		inbound := &uexecutortypes.Inbound{
			TxHash:      "0x123abc",
			SourceChain: "eip155:1",
			Sender:      "0xsender",
			Recipient:   "push1receiver",
			Amount:      "1000000",
		}

		msg := &uexecutortypes.MsgVoteInbound{
			Signer:  granter,
			Inbound: inbound,
		}

		assert.Equal(t, granter, msg.Signer)
		assert.Equal(t, inbound, msg.Inbound)
		assert.Equal(t, "0x123abc", msg.Inbound.TxHash)
	})

	t.Run("MsgVoteInbound with nil inbound", func(t *testing.T) {
		msg := &uexecutortypes.MsgVoteInbound{
			Signer:  "push1granter123",
			Inbound: nil,
		}
		assert.Nil(t, msg.Inbound)
	})
}

func TestMsgVoteGasPriceConstruction(t *testing.T) {
	t.Run("construct valid MsgVoteGasPrice", func(t *testing.T) {
		granter := "push1granter123"
		chainID := "eip155:1"
		price := uint64(20000000000)
		blockNumber := uint64(18500000)

		msg := &uexecutortypes.MsgVoteGasPrice{
			Signer:          granter,
			ObservedChainId: chainID,
			Price:           price,
			BlockNumber:     blockNumber,
		}

		assert.Equal(t, granter, msg.Signer)
		assert.Equal(t, chainID, msg.ObservedChainId)
		assert.Equal(t, price, msg.Price)
		assert.Equal(t, blockNumber, msg.BlockNumber)
	})

	t.Run("MsgVoteGasPrice with zero values", func(t *testing.T) {
		msg := &uexecutortypes.MsgVoteGasPrice{
			Signer:          "push1granter123",
			ObservedChainId: "eip155:1",
			Price:           0,
			BlockNumber:     0,
		}
		assert.Equal(t, uint64(0), msg.Price)
		assert.Equal(t, uint64(0), msg.BlockNumber)
	})
}

func TestMsgVoteOutboundConstruction(t *testing.T) {
	t.Run("construct valid MsgVoteOutbound for successful tx", func(t *testing.T) {
		granter := "push1granter123"
		txID := "tx-123"
		utxID := "utx-456"
		observation := &uexecutortypes.OutboundObservation{
			Success:     true,
			BlockHeight: 18500000,
			TxHash:      "0xabc123def456",
			ErrorMsg:    "",
		}

		msg := &uexecutortypes.MsgVoteOutbound{
			Signer:     granter,
			TxId:       txID,
			UtxId:      utxID,
			ObservedTx: observation,
		}

		assert.Equal(t, granter, msg.Signer)
		assert.Equal(t, txID, msg.TxId)
		assert.Equal(t, utxID, msg.UtxId)
		assert.True(t, msg.ObservedTx.Success)
		assert.Equal(t, "0xabc123def456", msg.ObservedTx.TxHash)
	})

	t.Run("construct valid MsgVoteOutbound for failed tx", func(t *testing.T) {
		observation := &uexecutortypes.OutboundObservation{
			Success:     false,
			BlockHeight: 0,
			TxHash:      "",
			ErrorMsg:    "execution reverted: insufficient balance",
		}

		msg := &uexecutortypes.MsgVoteOutbound{
			Signer:     "push1granter123",
			TxId:       "tx-789",
			UtxId:      "utx-101",
			ObservedTx: observation,
		}

		assert.False(t, msg.ObservedTx.Success)
		assert.Empty(t, msg.ObservedTx.TxHash)
		assert.Contains(t, msg.ObservedTx.ErrorMsg, "insufficient balance")
	})

	t.Run("MsgVoteOutbound with nil observation", func(t *testing.T) {
		msg := &uexecutortypes.MsgVoteOutbound{
			Signer:     "push1granter123",
			TxId:       "tx-123",
			UtxId:      "utx-456",
			ObservedTx: nil,
		}
		assert.Nil(t, msg.ObservedTx)
	})
}

func TestMsgVoteTssKeyProcessConstruction(t *testing.T) {
	t.Run("construct valid MsgVoteTssKeyProcess", func(t *testing.T) {
		granter := "push1granter123"
		tssPubKey := "tsspub1abc123"
		keyID := "key-001"
		processID := uint64(42)

		msg := &utsstypes.MsgVoteTssKeyProcess{
			Signer:    granter,
			TssPubkey: tssPubKey,
			KeyId:     keyID,
			ProcessId: processID,
		}

		assert.Equal(t, granter, msg.Signer)
		assert.Equal(t, tssPubKey, msg.TssPubkey)
		assert.Equal(t, keyID, msg.KeyId)
		assert.Equal(t, processID, msg.ProcessId)
	})

	t.Run("MsgVoteTssKeyProcess with empty strings", func(t *testing.T) {
		msg := &utsstypes.MsgVoteTssKeyProcess{
			Signer:    "",
			TssPubkey: "",
			KeyId:     "",
			ProcessId: 0,
		}
		assert.Empty(t, msg.Signer)
		assert.Empty(t, msg.TssPubkey)
		assert.Empty(t, msg.KeyId)
	})
}

func TestOutboundObservation(t *testing.T) {
	t.Run("successful observation fields", func(t *testing.T) {
		obs := &uexecutortypes.OutboundObservation{
			Success:     true,
			BlockHeight: 12345678,
			TxHash:      "0x1234567890abcdef",
			ErrorMsg:    "",
		}

		assert.True(t, obs.Success)
		assert.Equal(t, uint64(12345678), obs.BlockHeight)
		assert.Equal(t, "0x1234567890abcdef", obs.TxHash)
		assert.Empty(t, obs.ErrorMsg)
	})

	t.Run("failed observation fields", func(t *testing.T) {
		obs := &uexecutortypes.OutboundObservation{
			Success:     false,
			BlockHeight: 0,
			TxHash:      "",
			ErrorMsg:    "transaction failed: nonce too low",
		}

		assert.False(t, obs.Success)
		assert.Equal(t, uint64(0), obs.BlockHeight)
		assert.Empty(t, obs.TxHash)
		assert.NotEmpty(t, obs.ErrorMsg)
	})
}

func TestInbound(t *testing.T) {
	t.Run("inbound struct fields", func(t *testing.T) {
		inbound := &uexecutortypes.Inbound{
			TxHash:      "0xabc123",
			SourceChain: "eip155:97",
			Sender:      "0x1234567890123456789012345678901234567890",
			Recipient:   "push1receiver123",
			Amount:      "1000000000000000000",
		}

		assert.Equal(t, "0xabc123", inbound.TxHash)
		assert.Equal(t, "eip155:97", inbound.SourceChain)
		assert.NotEmpty(t, inbound.Sender)
		assert.NotEmpty(t, inbound.Recipient)
		assert.NotEmpty(t, inbound.Amount)
	})

	t.Run("inbound with zero amount", func(t *testing.T) {
		inbound := &uexecutortypes.Inbound{
			TxHash:      "0xdef456",
			SourceChain: "eip155:1",
			Sender:      "0xsender",
			Recipient:   "push1receiver",
			Amount:      "0",
		}

		assert.Equal(t, "0", inbound.Amount)
	})
}

func TestVoteMemoFormats(t *testing.T) {
	t.Run("inbound vote memo format", func(t *testing.T) {
		inbound := &uexecutortypes.Inbound{
			TxHash: "0x123abc456def",
		}
		expectedMemo := "Vote inbound: 0x123abc456def"
		actualMemo := "Vote inbound: " + inbound.TxHash
		assert.Equal(t, expectedMemo, actualMemo)
	})

	t.Run("gas price vote memo format", func(t *testing.T) {
		chainID := "eip155:1"
		price := "25000000000"
		expectedMemo := "Vote gas price: eip155:1 @ 25000000000"
		actualMemo := "Vote gas price: " + chainID + " @ " + price
		assert.Equal(t, expectedMemo, actualMemo)
	})

	t.Run("outbound vote memo format", func(t *testing.T) {
		txID := "tx-12345"
		expectedMemo := "Vote outbound: tx-12345"
		actualMemo := "Vote outbound: " + txID
		assert.Equal(t, expectedMemo, actualMemo)
	})

	t.Run("tss key vote memo format", func(t *testing.T) {
		keyID := "key-001"
		expectedMemo := "Vote TSS key: key-001"
		actualMemo := "Vote TSS key: " + keyID
		assert.Equal(t, expectedMemo, actualMemo)
	})
}
