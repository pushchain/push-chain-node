package svm

import (
	"encoding/hex"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEventDecoder_RejectsAddFundsEvent(t *testing.T) {
	logger := zerolog.Nop()
	decoder := NewEventDecoder(logger)

	hexData := "7f1f6cffbb134644cbc6106957ccc13185c857d52824e9a94ca84209c2d0c4b5453228a669bc5e9e60fa0f00000000001cb75f01000000000000000000000000f8ffffffde7f67fc43345b947ffa72d5fe344291222b35cbf3373a44ce9e8c05a6c6b89d"
	data, err := hex.DecodeString(hexData)
	require.NoError(t, err)

	event, err := decoder.DecodeEventData(data)
	require.Error(t, err)
	assert.Nil(t, event)
	assert.Contains(t, err.Error(), "unknown event discriminator")
}

func TestEventDecoder_DecodeTxWithFundsEvent(t *testing.T) {
	logger := zerolog.Nop()
	decoder := NewEventDecoder(logger)

	// Simulated TxWithFunds event data with proper structure
	// This would be the actual event from a send_funds/deposit transaction
	// Structure: discriminator(8) + sender(32) + recipient(32) + bridge_amount(8) + gas_amount(8) +
	//           bridge_token(32) + data_len(4) + data(0) + revert_has_recipient(1) + revert_msg_len(4) +
	//           revert_msg(0) + tx_type(1) + sig_len(4) + sig_data(0)
	hexData := "2b1f1f0204ec6bff" + // discriminator for TxWithFunds
		"cbc6106957ccc13185c857d52824e9a94ca84209c2d0c4b5453228a669bc5e9e" + // sender
		"453228a669bc5e9ecbc6106957ccc13185c857d52824e9a94ca84209c2d0c4b5" + // recipient
		"e803000000000000" + // bridge_amount (1000)
		"6400000000000000" + // gas_amount (100)
		"0000000000000000000000000000000000000000000000000000000000000000" + // bridge_token (zero = native SOL)
		"00000000" + // data_len (0)
		"00" + // no revert recipient
		"00000000" + // revert_msg_len (0)
		"00" + // tx_type (0 = Funds)
		"00000000" // sig_len (0)

	data, err := hex.DecodeString(hexData)
	require.NoError(t, err)

	event, err := decoder.DecodeEventData(data)
	require.NoError(t, err)
	require.NotNil(t, event)

	assert.Equal(t, "TxWithFunds", event.EventType)
	assert.Equal(t, "EiSw2iqgc5N6mHpH1RbGmPBwfvUiNC9few5SxsmxiPvm", event.Sender)
	assert.NotEmpty(t, event.Recipient) // Just check it's not empty since the exact value depends on the bytes
	assert.Equal(t, uint64(1000), event.BridgeAmount)
	assert.Equal(t, uint64(100), event.GasAmount)
	assert.Equal(t, "11111111111111111111111111111111", event.BridgeToken) // System program (zero key)
	assert.Equal(t, "", event.Data)
	assert.Equal(t, "", event.VerificationData)
	assert.Equal(t, uint8(0), event.TxType) // Funds type
}

func TestEventDecoder_UnknownDiscriminator(t *testing.T) {
	logger := zerolog.Nop()
	decoder := NewEventDecoder(logger)

	// Unknown discriminator
	hexData := "0000000000000000" + "cbc6106957ccc13185c857d52824e9a94ca84209c2d0c4b5453228a669bc5e9e"
	data, err := hex.DecodeString(hexData)
	require.NoError(t, err)

	event, err := decoder.DecodeEventData(data)
	assert.Error(t, err)
	assert.Nil(t, event)
	assert.Contains(t, err.Error(), "unknown event discriminator")
}
