package svm

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"testing"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/universalClient/chains/common"
	uregistrytypes "github.com/pushchain/push-chain-node/x/uregistry/types"
)

func TestNewEventParser(t *testing.T) {
	tests := []struct {
		name       string
		config     *uregistrytypes.ChainConfig
		wantTopics int
	}{
		{
			name: "creates parser with event topics",
			config: &uregistrytypes.ChainConfig{
				Chain:          "solana-testnet",
				GatewayAddress: "11111111111111111111111111111111",
				GatewayMethods: []*uregistrytypes.GatewayMethods{
					{
						Identifier:      "method1",
						Name:            "send_funds",
						EventIdentifier: "2b1f1f0204ec6bff",
					},
					{
						Identifier:      "method2",
						Name:            "withdraw_funds",
						EventIdentifier: "abcdef1234567890",
					},
				},
			},
			wantTopics: 2,
		},
		{
			name: "handles methods without event identifiers",
			config: &uregistrytypes.ChainConfig{
				Chain:          "solana-testnet",
				GatewayAddress: "11111111111111111111111111111111",
				GatewayMethods: []*uregistrytypes.GatewayMethods{
					{
						Identifier:      "method1",
						Name:            "send_funds",
						EventIdentifier: "2b1f1f0204ec6bff",
					},
					{
						Identifier: "method2",
						Name:       "noEvent",
						// No EventIdentifier
					},
				},
			},
			wantTopics: 1,
		},
		{
			name: "handles empty gateway methods",
			config: &uregistrytypes.ChainConfig{
				Chain:          "solana-testnet",
				GatewayAddress: "11111111111111111111111111111111",
				GatewayMethods: []*uregistrytypes.GatewayMethods{},
			},
			wantTopics: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := zerolog.Nop()
			gatewayAddr := solana.PublicKeyFromBytes(make([]byte, 32))

			parser := NewEventParser(gatewayAddr, tt.config, logger)

			require.NotNil(t, parser)
			assert.Equal(t, tt.wantTopics, len(parser.eventTopics))
			assert.Equal(t, gatewayAddr, parser.gatewayAddr)
			assert.Equal(t, tt.config, parser.config)
		})
	}
}

func TestParseGatewayEvent(t *testing.T) {
	logger := zerolog.Nop()
	gatewayAddr := solana.PublicKeyFromBytes(make([]byte, 32))

	config := &uregistrytypes.ChainConfig{
		Chain: "solana-testnet",
		GatewayMethods: []*uregistrytypes.GatewayMethods{
			{
				Identifier:      "method1",
				Name:            "send_funds",
				EventIdentifier: UniversalTxDiscriminator,
			},
		},
	}

	parser := NewEventParser(gatewayAddr, config, logger)

	// Create test event data
	sender := solana.PublicKeyFromBytes(make([]byte, 32))
	recipient := make([]byte, 20)
	bridgeToken := solana.PublicKeyFromBytes(make([]byte, 32))
	revertRecipient := solana.PublicKeyFromBytes(make([]byte, 32))

	tests := []struct {
		name      string
		tx        *rpc.GetTransactionResult
		signature string
		slot      uint64
		wantEvent bool
		validate  func(*testing.T, *common.GatewayEvent)
	}{
		{
			name: "parses valid gateway event",
			tx: func() *rpc.GetTransactionResult {
				eventData := createTestEventData(
					UniversalTxDiscriminator,
					sender[:],
					recipient,
					1000000,
					bridgeToken[:],
					"test-data",
					revertRecipient[:],
					"revert-message",
					2, // tx type
					"signature-data",
				)
				encodedData := base64.StdEncoding.EncodeToString(eventData)
				return &rpc.GetTransactionResult{
					Meta: &rpc.TransactionMeta{
						LogMessages: []string{
							"Program data: " + encodedData,
						},
					},
				}
			}(),
			signature: "test-signature",
			slot:      12345,
			wantEvent: true,
			validate: func(t *testing.T, event *common.GatewayEvent) {
				assert.Equal(t, "solana-testnet", event.ChainID)
				assert.Equal(t, "test-signature", event.TxHash)
				assert.Equal(t, uint64(12345), event.BlockNumber)
				assert.Equal(t, UniversalTxDiscriminator, event.EventID)
				assert.Equal(t, "STANDARD", event.ConfirmationType) // TxType 2 = STANDARD

				var payload common.UniversalTx
				err := json.Unmarshal(event.Payload, &payload)
				require.NoError(t, err)
				assert.Equal(t, "1000000", payload.Amount)
				assert.Equal(t, uint(2), payload.TxType)
			},
		},
		{
			name:      "returns nil for nil transaction",
			tx:        nil,
			wantEvent: false,
		},
		{
			name: "returns nil for transaction with no meta",
			tx: &rpc.GetTransactionResult{
				Meta: nil,
			},
			wantEvent: false,
		},
		{
			name: "returns nil for no matching event",
			tx: func() *rpc.GetTransactionResult {
				eventData := createTestEventData(
					"wrong-discriminator",
					sender[:],
					recipient,
					1000,
					bridgeToken[:],
					"",
					revertRecipient[:],
					"",
					0,
					"",
				)
				encodedData := base64.StdEncoding.EncodeToString(eventData)
				return &rpc.GetTransactionResult{
					Meta: &rpc.TransactionMeta{
						LogMessages: []string{
							"Program data: " + encodedData,
						},
					},
				}
			}(),
			wantEvent: false,
		},
		{
			name: "filters addFunds discriminator",
			tx: func() *rpc.GetTransactionResult {
				eventData := createTestEventData(
					AddFundsDiscriminator,
					sender[:],
					recipient,
					1000,
					bridgeToken[:],
					"",
					revertRecipient[:],
					"",
					0,
					"",
				)
				encodedData := base64.StdEncoding.EncodeToString(eventData)
				return &rpc.GetTransactionResult{
					Meta: &rpc.TransactionMeta{
						LogMessages: []string{
							"Program data: " + encodedData,
						},
					},
				}
			}(),
			wantEvent: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := parser.ParseGatewayEvent(tt.tx, tt.signature, tt.slot)

			if tt.wantEvent {
				require.NotNil(t, event)
				if tt.validate != nil {
					tt.validate(t, event)
				}
			} else {
				assert.Nil(t, event)
			}
		})
	}
}

func TestGetEventTopics(t *testing.T) {
	config := &uregistrytypes.ChainConfig{
		GatewayMethods: []*uregistrytypes.GatewayMethods{
			{
				Identifier:      "method1",
				Name:            "send_funds",
				EventIdentifier: "discriminator1",
			},
			{
				Identifier:      "method2",
				Name:            "send_funds_fast",
				EventIdentifier: "discriminator2",
			},
		},
	}

	logger := zerolog.Nop()
	gatewayAddr := solana.PublicKeyFromBytes(make([]byte, 32))
	parser := NewEventParser(gatewayAddr, config, logger)

	topics := parser.GetEventTopics()

	assert.Len(t, topics, 2)
	assert.Contains(t, topics, "discriminator1")
	assert.Contains(t, topics, "discriminator2")
}

func TestDecodeUniversalTxEvent(t *testing.T) {
	logger := zerolog.Nop()
	gatewayAddr := solana.PublicKeyFromBytes(make([]byte, 32))
	parser := NewEventParser(gatewayAddr, &uregistrytypes.ChainConfig{}, logger)

	sender := solana.PublicKeyFromBytes(make([]byte, 32))
	recipient := make([]byte, 20)
	bridgeToken := solana.PublicKeyFromBytes(make([]byte, 32))
	revertRecipient := solana.PublicKeyFromBytes(make([]byte, 32))

	tests := []struct {
		name      string
		data      []byte
		wantError bool
		validate  func(*testing.T, *common.UniversalTx)
	}{
		{
			name: "decodes valid event data",
			data: createTestEventData(
				UniversalTxDiscriminator,
				sender[:],
				recipient,
				1000000,
				bridgeToken[:],
				"test-data",
				revertRecipient[:],
				"revert-message",
				2,
				"signature-data",
			),
			wantError: false,
			validate: func(t *testing.T, payload *common.UniversalTx) {
				assert.Equal(t, "1000000", payload.Amount)
				assert.Equal(t, "0x"+hex.EncodeToString(recipient), payload.Recipient)
				assert.Equal(t, uint(2), payload.TxType)
				assert.Equal(t, "0x7369676e61747572652d64617461", payload.VerificationData)
				assert.Equal(t, "0x7265766572742d6d657373616765", payload.RevertMsg)
			},
		},
		{
			name: "handles empty fields",
			data: createTestEventData(
				UniversalTxDiscriminator,
				sender[:],
				recipient,
				1000000,
				bridgeToken[:],
				"",
				revertRecipient[:],
				"",
				2,
				"",
			),
			wantError: false,
			validate: func(t *testing.T, payload *common.UniversalTx) {
				assert.Equal(t, "", payload.VerificationData)
				assert.Equal(t, "", payload.RevertMsg)
			},
		},
		{
			name:      "handles insufficient data",
			data:      []byte("short"),
			wantError: true,
		},
		{
			name: "accepts any tx type",
			data: func() []byte {
				data := make([]byte, 0)
				discBytes, _ := hex.DecodeString(UniversalTxDiscriminator)
				data = append(data, discBytes...)
				data = append(data, make([]byte, 32)...) // sender
				data = append(data, make([]byte, 20)...) // recipient
				data = append(data, make([]byte, 32)...) // bridge_token
				bridgeAmount := make([]byte, 8)
				binary.LittleEndian.PutUint64(bridgeAmount, 1000000)
				data = append(data, bridgeAmount...)
				dataLen := make([]byte, 4)
				binary.LittleEndian.PutUint32(dataLen, 0)
				data = append(data, dataLen...)
				data = append(data, make([]byte, 32)...) // revert_recipient
				revertMsgLen := make([]byte, 4)
				binary.LittleEndian.PutUint32(revertMsgLen, 0)
				data = append(data, revertMsgLen...)
				data = append(data, byte(5)) // tx type 5
				sigLen := make([]byte, 4)
				binary.LittleEndian.PutUint32(sigLen, 0)
				data = append(data, sigLen...)
				return data
			}(),
			wantError: false,
			validate: func(t *testing.T, payload *common.UniversalTx) {
				assert.Equal(t, uint(5), payload.TxType)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload, err := parser.decodeUniversalTxEvent(tt.data)

			if tt.wantError {
				assert.Error(t, err)
				assert.Nil(t, payload)
			} else {
				require.NoError(t, err)
				require.NotNil(t, payload)
				if tt.validate != nil {
					tt.validate(t, payload)
				}
			}
		})
	}
}

func TestDecodeUniversalPayload(t *testing.T) {
	tests := []struct {
		name      string
		hexStr    string
		wantError bool
		wantNil   bool
	}{
		{
			name:      "handles empty string",
			hexStr:    "",
			wantError: false,
			wantNil:   true,
		},
		{
			name:      "handles invalid hex",
			hexStr:    "invalid-hex",
			wantError: true,
			wantNil:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := decodeUniversalPayload(tt.hexStr)

			if tt.wantError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			if tt.wantNil {
				assert.Nil(t, result)
			}
		})
	}
}

// Helper function for creating test event data
// Format: discriminator (8), sender (32), recipient (20), bridge_token (32), bridge_amount (8),
//
//	data_len (4) + data, revert_recipient (32), revert_msg_len (4) + revert_msg, tx_type (1), sig_len (4) + sig_data
func createTestEventData(
	discriminator string,
	sender, recipient []byte,
	bridgeAmount uint64,
	bridgeToken []byte,
	data string,
	revertRecipient []byte,
	revertMessage string,
	txType uint8,
	signatureData string,
) []byte {
	discBytes, _ := hex.DecodeString(discriminator)

	dataBytes := make([]byte, 0)
	dataBytes = append(dataBytes, discBytes...)
	dataBytes = append(dataBytes, sender...)
	dataBytes = append(dataBytes, recipient...)
	dataBytes = append(dataBytes, bridgeToken...)

	bridgeAmountBytes := make([]byte, 8)
	binary.LittleEndian.PutUint64(bridgeAmountBytes, bridgeAmount)
	dataBytes = append(dataBytes, bridgeAmountBytes...)

	dataLen := uint32(len(data))
	dataLenBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(dataLenBytes, dataLen)
	dataBytes = append(dataBytes, dataLenBytes...)
	if dataLen > 0 {
		dataBytes = append(dataBytes, []byte(data)...)
	}

	dataBytes = append(dataBytes, revertRecipient...)

	revertMsgLen := uint32(len(revertMessage))
	revertMsgLenBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(revertMsgLenBytes, revertMsgLen)
	dataBytes = append(dataBytes, revertMsgLenBytes...)
	if revertMsgLen > 0 {
		dataBytes = append(dataBytes, []byte(revertMessage)...)
	}

	dataBytes = append(dataBytes, txType)

	sigLen := uint32(len(signatureData))
	sigLenBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(sigLenBytes, sigLen)
	dataBytes = append(dataBytes, sigLenBytes...)
	if sigLen > 0 {
		dataBytes = append(dataBytes, []byte(signatureData)...)
	}

	return dataBytes
}
