package evm

import (
	"encoding/hex"
	"encoding/json"
	"math/big"
	"testing"

	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/universalClient/chains/common"
	"github.com/pushchain/push-chain-node/universalClient/store"
	uregistrytypes "github.com/pushchain/push-chain-node/x/uregistry/types"
)

func TestParseGatewayEvent(t *testing.T) {
	gatewayAddr := ethcommon.HexToAddress("0x742d35Cc6634C0532925a3b844Bc9e7595f0bEb7")
	// Use a different event topic (not AddFundsEventID which is filtered)
	eventTopic := ethcommon.HexToHash("0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef")

	config := &uregistrytypes.ChainConfig{
		Chain:          "eip155:1",
		GatewayAddress: gatewayAddr.Hex(),
		GatewayMethods: []*uregistrytypes.GatewayMethods{
			{
				Name:            "sendFunds",
				Identifier:      "method1",
				EventIdentifier: "0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
			},
		},
	}

	logger := zerolog.New(nil).Level(zerolog.Disabled)

	tests := []struct {
		name      string
		log       *types.Log
		eventType string
		wantEvent bool
		validate  func(*testing.T, *store.Event)
	}{
		{
			name:      "parses valid sendFunds event",
			eventType: EventTypeSendFunds,
			log: &types.Log{
				Address: gatewayAddr,
				Topics: []ethcommon.Hash{
					eventTopic,
					ethcommon.HexToHash("0x000000000000000000000000742d35cc6634c0532925a3b844bc9e7595f0beb7"), // sender
					ethcommon.HexToHash("0x000000000000000000000000dac17f958d2ee523a2206206994597c13d831ec7"), // recipient
				},
				Data: func() []byte {
					data := make([]byte, 160) // 5 words minimum for sendFunds event
					// Word 0: bridgeToken
					copy(data[12:32], ethcommon.HexToAddress("0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48").Bytes())
					// Word 1: bridgeAmount
					big.NewInt(1000000).FillBytes(data[32:64])
					return data
				}(),
				TxHash:      ethcommon.HexToHash("0xabc123"),
				Index:       0,
				BlockNumber: 12345,
			},
			wantEvent: true,
			validate: func(t *testing.T, event *store.Event) {
				// TxHash.Hex() returns full 32-byte hex representation
				assert.Contains(t, event.EventID, "abc123")
				assert.Contains(t, event.EventID, ":0")
				assert.Equal(t, uint64(12345), event.BlockHeight)
			},
		},
		{
			name:      "parses sendFunds event with topics",
			eventType: EventTypeSendFunds,
			log: &types.Log{
				Address: gatewayAddr,
				Topics: []ethcommon.Hash{
					eventTopic,
					ethcommon.HexToHash("0x000000000000000000000000742d35cc6634c0532925a3b844bc9e7595f0beb7"), // sender
					ethcommon.HexToHash("0x000000000000000000000000dac17f958d2ee523a2206206994597c13d831ec7"), // recipient
				},
				Data: func() []byte {
					data := make([]byte, 160) // 5 words for sendFunds event
					// Word 0: bridgeToken
					copy(data[12:32], ethcommon.HexToAddress("0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48").Bytes())
					// Word 1: bridgeAmount
					big.NewInt(1000000).FillBytes(data[32:64])
					return data
				}(),
				TxHash:      ethcommon.HexToHash("0xdef456"),
				BlockNumber: 12346,
			},
			wantEvent: true,
			validate: func(t *testing.T, event *store.Event) {
				assert.Equal(t, uint64(12346), event.BlockHeight)
			},
		},
		{
			name:      "returns nil for outboundObservation with insufficient topics",
			eventType: EventTypeFinalizeUniversalTx,
			log: &types.Log{
				Address: gatewayAddr,
				Topics:  []ethcommon.Hash{eventTopic}, // Only 1 topic, need 3 for outbound
				Data:    []byte{},
				TxHash:  ethcommon.HexToHash("0xabc789"),
			},
			wantEvent: false, // Needs at least 3 topics (event signature + txID + universalTxID)
		},
		{
			name:      "returns nil for log with no topics",
			eventType: EventTypeSendFunds,
			log: &types.Log{
				Address: gatewayAddr,
				Topics:  []ethcommon.Hash{},
				Data:    []byte{},
			},
			wantEvent: false,
		},
		{
			name:      "returns nil for unknown event type",
			eventType: "unknown",
			log: &types.Log{
				Address: gatewayAddr,
				Topics:  []ethcommon.Hash{eventTopic},
				Data:    []byte{},
			},
			wantEvent: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := ParseEvent(tt.log, tt.eventType, config.Chain, logger)

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

func TestParseEventData(t *testing.T) {
	config := &uregistrytypes.ChainConfig{
		Chain: "eip155:1",
		GatewayMethods: []*uregistrytypes.GatewayMethods{
			{
				Name:            "sendFunds",
				Identifier:      "method1",
				EventIdentifier: "0x1234",
			},
		},
	}

	logger := zerolog.New(nil).Level(zerolog.Disabled)

	t.Run("parses sendFunds event data correctly", func(t *testing.T) {
		// Create amount data (32 bytes for uint256)
		amount := big.NewInt(1000000)
		amountBytes := make([]byte, 160)     // 5 words minimum
		amount.FillBytes(amountBytes[32:64]) // Word 1: bridgeAmount

		log := &types.Log{
			Topics: []ethcommon.Hash{
				ethcommon.HexToHash("0x1234"), // event signature
				ethcommon.HexToHash("0x000000000000000000000000742d35cc6634c0532925a3b844bc9e7595f0beb7"), // sender
				ethcommon.HexToHash("0x000000000000000000000000dac17f958d2ee523a2206206994597c13d831ec7"), // receiver
			},
			Data: amountBytes,
		}

		event := ParseEvent(log, EventTypeSendFunds, config.Chain, logger)
		require.NotNil(t, event)
		assert.NotNil(t, event.EventData)
	})

	t.Run("handles missing data gracefully", func(t *testing.T) {
		log := &types.Log{
			Topics: []ethcommon.Hash{
				ethcommon.HexToHash("0x1234"),
				ethcommon.HexToHash("0x000000000000000000000000742d35cc6634c0532925a3b844bc9e7595f0beb7"), // sender
				ethcommon.HexToHash("0x000000000000000000000000dac17f958d2ee523a2206206994597c13d831ec7"), // receiver
			},
			Data: []byte{}, // Empty data
		}

		event := ParseEvent(log, EventTypeSendFunds, config.Chain, logger)
		// Should still create event but with minimal data
		require.NotNil(t, event)
	})
}

func TestParseOutboundObservationEvent(t *testing.T) {
	logger := zerolog.New(nil).Level(zerolog.Disabled)
	chainID := "eip155:1"

	// Example bytes32 values
	txIDBytes := ethcommon.HexToHash("0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef")
	universalTxIDBytes := ethcommon.HexToHash("0xfedcba0987654321fedcba0987654321fedcba0987654321fedcba0987654321")
	eventSignature := ethcommon.HexToHash("0xabcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234")

	tests := []struct {
		name      string
		log       *types.Log
		wantEvent bool
		validate  func(*testing.T, *store.Event)
	}{
		{
			name: "parses valid outbound observation event",
			log: &types.Log{
				Topics: []ethcommon.Hash{
					eventSignature,     // Topics[0]: event signature
					txIDBytes,          // Topics[1]: txID (bytes32)
					universalTxIDBytes, // Topics[2]: universalTxID (bytes32)
				},
				Data:        []byte{},
				TxHash:      ethcommon.HexToHash("0xabc123def456"),
				Index:       5,
				BlockNumber: 98765,
			},
			wantEvent: true,
			validate: func(t *testing.T, event *store.Event) {
				// TxHash.Hex() returns full 32-byte hex representation
				assert.Equal(t, "0x0000000000000000000000000000000000000000000000000000abc123def456:5", event.EventID)
				assert.Equal(t, uint64(98765), event.BlockHeight)
				assert.Equal(t, store.EventTypeOutbound, event.Type)
				assert.Equal(t, store.StatusPending, event.Status)
				assert.Equal(t, store.ConfirmationStandard, event.ConfirmationType)

				// Verify event data contains tx_id and universal_tx_id
				assert.NotNil(t, event.EventData)
				var outboundData map[string]interface{}
				err := json.Unmarshal(event.EventData, &outboundData)
				require.NoError(t, err)

				assert.Equal(t, "0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef", outboundData["tx_id"])
				assert.Equal(t, "0xfedcba0987654321fedcba0987654321fedcba0987654321fedcba0987654321", outboundData["universal_tx_id"])
			},
		},
		{
			name: "returns nil for log with insufficient topics (only 2)",
			log: &types.Log{
				Topics: []ethcommon.Hash{
					eventSignature,
					txIDBytes,
					// Missing universalTxID
				},
				Data:        []byte{},
				TxHash:      ethcommon.HexToHash("0xdef789"),
				BlockNumber: 12345,
			},
			wantEvent: false,
		},
		{
			name: "returns nil for log with only event signature",
			log: &types.Log{
				Topics: []ethcommon.Hash{
					eventSignature,
				},
				Data:        []byte{},
				TxHash:      ethcommon.HexToHash("0x111222"),
				BlockNumber: 12345,
			},
			wantEvent: false,
		},
		{
			name: "returns nil for log with no topics",
			log: &types.Log{
				Topics:      []ethcommon.Hash{},
				Data:        []byte{},
				TxHash:      ethcommon.HexToHash("0x333444"),
				BlockNumber: 12345,
			},
			wantEvent: false,
		},
		{
			name: "correctly formats txID and universalTxID as hex strings",
			log: &types.Log{
				Topics: []ethcommon.Hash{
					eventSignature,
					ethcommon.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000001"), // txID = 1
					ethcommon.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000002"), // universalTxID = 2
				},
				Data:        []byte{},
				TxHash:      ethcommon.HexToHash("0x555666"),
				Index:       0,
				BlockNumber: 54321,
			},
			wantEvent: true,
			validate: func(t *testing.T, event *store.Event) {
				var outboundData map[string]interface{}
				err := json.Unmarshal(event.EventData, &outboundData)
				require.NoError(t, err)

				// Values should be full 32-byte hex strings with 0x prefix
				assert.Equal(t, "0x0000000000000000000000000000000000000000000000000000000000000001", outboundData["tx_id"])
				assert.Equal(t, "0x0000000000000000000000000000000000000000000000000000000000000002", outboundData["universal_tx_id"])
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := ParseEvent(tt.log, EventTypeFinalizeUniversalTx, chainID, logger)

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

func TestParseGatewayEvent_OutboundObservation(t *testing.T) {
	gatewayAddr := ethcommon.HexToAddress("0x742d35Cc6634C0532925a3b844Bc9e7595f0bEb7")
	outboundTopic := ethcommon.HexToHash("0x9876543210fedcba9876543210fedcba9876543210fedcba9876543210fedcba")

	config := &uregistrytypes.ChainConfig{
		Chain:          "eip155:1",
		GatewayAddress: gatewayAddr.Hex(),
		GatewayMethods: []*uregistrytypes.GatewayMethods{
			{
				Name:            "outboundObservation",
				Identifier:      "outbound_method",
				EventIdentifier: "0x9876543210fedcba9876543210fedcba9876543210fedcba9876543210fedcba",
			},
		},
	}

	logger := zerolog.New(nil).Level(zerolog.Disabled)

	t.Run("parses outbound observation event correctly", func(t *testing.T) {
		log := &types.Log{
			Address: gatewayAddr,
			Topics: []ethcommon.Hash{
				outboundTopic,
				ethcommon.HexToHash("0xaaaa000000000000000000000000000000000000000000000000000000000001"), // txID
				ethcommon.HexToHash("0xbbbb000000000000000000000000000000000000000000000000000000000002"), // universalTxID
			},
			Data:        []byte{},
			TxHash:      ethcommon.HexToHash("0xoutbound123"),
			Index:       3,
			BlockNumber: 77777,
		}

		event := ParseEvent(log, EventTypeFinalizeUniversalTx, config.Chain, logger)
		require.NotNil(t, event)

		assert.Equal(t, store.EventTypeOutbound, event.Type)
		assert.Equal(t, store.ConfirmationStandard, event.ConfirmationType)
		assert.Equal(t, uint64(77777), event.BlockHeight)

		var outboundData map[string]interface{}
		err := json.Unmarshal(event.EventData, &outboundData)
		require.NoError(t, err)

		assert.Contains(t, outboundData["tx_id"], "0xaaaa")
		assert.Contains(t, outboundData["universal_tx_id"], "0xbbbb")
	})
}

// ---------------------------------------------------------------------------
// readDynamicBytes
// ---------------------------------------------------------------------------

func TestReadDynamicBytes(t *testing.T) {
	t.Run("valid offset with normal bytes", func(t *testing.T) {
		// Build ABI-encoded dynamic bytes: first 32 bytes = length, then the data.
		inner := []byte{0xca, 0xfe, 0xba, 0xbe}
		data := make([]byte, 64) // 32 (length) + 32 (padded data)
		big.NewInt(int64(len(inner))).FillBytes(data[0:32])
		copy(data[32:36], inner)

		hexStr, ok := readDynamicBytes(data, 0)
		assert.True(t, ok)
		assert.Equal(t, "0xcafebabe", hexStr)
	})

	t.Run("zero-length bytes", func(t *testing.T) {
		data := make([]byte, 32) // length = 0
		hexStr, ok := readDynamicBytes(data, 0)
		assert.True(t, ok)
		assert.Equal(t, "0x", hexStr)
	})

	t.Run("offset exactly at boundary", func(t *testing.T) {
		// data has 64 bytes; offset 32 points to second word which encodes length=0
		data := make([]byte, 64)
		hexStr, ok := readDynamicBytes(data, 32)
		assert.True(t, ok)
		assert.Equal(t, "0x", hexStr)
	})

	t.Run("out-of-bounds offset past data length", func(t *testing.T) {
		data := make([]byte, 16) // too short for a 32-byte length word
		_, ok := readDynamicBytes(data, 0)
		assert.False(t, ok)
	})

	t.Run("offset beyond data", func(t *testing.T) {
		data := make([]byte, 32)
		_, ok := readDynamicBytes(data, 64)
		assert.False(t, ok)
	})

	t.Run("length exceeds remaining data", func(t *testing.T) {
		// length says 100, but only 4 bytes of actual data follow
		data := make([]byte, 64)
		big.NewInt(100).FillBytes(data[0:32])
		_, ok := readDynamicBytes(data, 0)
		assert.False(t, ok)
	})

	t.Run("multi-word data extraction", func(t *testing.T) {
		inner := make([]byte, 40) // spans more than one 32-byte word
		for i := range inner {
			inner[i] = byte(i)
		}
		data := make([]byte, 32+64) // length word + 2 padded words
		big.NewInt(int64(len(inner))).FillBytes(data[0:32])
		copy(data[32:], inner)

		hexStr, ok := readDynamicBytes(data, 0)
		assert.True(t, ok)
		expected := "0x" + hex.EncodeToString(inner)
		assert.Equal(t, expected, hexStr)
	})
}

// ---------------------------------------------------------------------------
// readWord
// ---------------------------------------------------------------------------

func TestReadWord(t *testing.T) {
	data := make([]byte, 96) // 3 words
	for i := range data {
		data[i] = byte(i)
	}

	t.Run("first word", func(t *testing.T) {
		w := readWord(data, 0)
		require.NotNil(t, w)
		assert.Len(t, w, 32)
		assert.Equal(t, byte(0), w[0])
	})

	t.Run("second word", func(t *testing.T) {
		w := readWord(data, 1)
		require.NotNil(t, w)
		assert.Equal(t, byte(32), w[0])
	})

	t.Run("third word", func(t *testing.T) {
		w := readWord(data, 2)
		require.NotNil(t, w)
		assert.Equal(t, byte(64), w[0])
	})

	t.Run("out of bounds returns nil", func(t *testing.T) {
		w := readWord(data, 3)
		assert.Nil(t, w)
	})

	t.Run("negative index returns nil", func(t *testing.T) {
		w := readWord(data, -1)
		assert.Nil(t, w)
	})

	t.Run("empty data returns nil", func(t *testing.T) {
		w := readWord([]byte{}, 0)
		assert.Nil(t, w)
	})
}

// ---------------------------------------------------------------------------
// decodePayload
// ---------------------------------------------------------------------------

func TestDecodePayload(t *testing.T) {
	logger := zerolog.New(nil).Level(zerolog.Disabled)

	t.Run("valid payload at correct offset", func(t *testing.T) {
		// Build data large enough: need dataOffset >= 32*5 = 160
		// At dataOffset, place ABI-encoded dynamic bytes (length + data).
		inner := []byte{0xde, 0xad, 0xbe, 0xef}
		data := make([]byte, 224) // 7 words
		// Place dynamic bytes at offset 160 (word 5)
		big.NewInt(int64(len(inner))).FillBytes(data[160:192])
		copy(data[192:196], inner)

		payload := &common.UniversalTx{}
		decodePayload(data, 160, payload, logger)
		assert.Equal(t, "0xdeadbeef", payload.RawPayload)
	})

	t.Run("offset too small is ignored", func(t *testing.T) {
		data := make([]byte, 256)
		payload := &common.UniversalTx{}
		decodePayload(data, 32, payload, logger) // < 32*5
		assert.Empty(t, payload.RawPayload)
	})

	t.Run("offset zero is ignored", func(t *testing.T) {
		data := make([]byte, 256)
		payload := &common.UniversalTx{}
		decodePayload(data, 0, payload, logger)
		assert.Empty(t, payload.RawPayload)
	})

	t.Run("readDynamicBytes fails gracefully", func(t *testing.T) {
		// Data is too short for the length word at the offset
		data := make([]byte, 168) // offset 160 + only 8 bytes; need 32 for length
		payload := &common.UniversalTx{}
		decodePayload(data, 160, payload, logger)
		assert.Empty(t, payload.RawPayload)
	})
}

// ---------------------------------------------------------------------------
// decodeSignatureData
// ---------------------------------------------------------------------------

func TestDecodeSignatureData(t *testing.T) {
	t.Run("dynamic bytes at valid offset", func(t *testing.T) {
		// Build data with dynamic bytes at offset 224 (word 7)
		data := make([]byte, 288) // 9 words
		inner := []byte{0x01, 0x02, 0x03}
		big.NewInt(int64(len(inner))).FillBytes(data[224:256])
		copy(data[256:259], inner)

		// w encodes offset 224
		w := make([]byte, 32)
		big.NewInt(224).FillBytes(w)

		result := decodeSignatureData(data, w, 224)
		assert.Equal(t, "0x010203", result)
	})

	t.Run("offset below minOffset falls back to fixed bytes32", func(t *testing.T) {
		data := make([]byte, 256)
		w := make([]byte, 32)
		big.NewInt(100).FillBytes(w) // offset 100 < minOffset 224

		result := decodeSignatureData(data, w, 224)
		// Fallback: treat w as fixed bytes32
		assert.Equal(t, "0x"+hex.EncodeToString(w), result)
	})

	t.Run("offset beyond data falls back to fixed bytes32", func(t *testing.T) {
		data := make([]byte, 64)
		w := make([]byte, 32)
		big.NewInt(1000).FillBytes(w) // offset 1000 > len(data)

		result := decodeSignatureData(data, w, 0)
		assert.Equal(t, "0x"+hex.EncodeToString(w), result)
	})

	t.Run("readDynamicBytes fails falls back to fixed bytes32", func(t *testing.T) {
		// Offset is valid range but the dynamic bytes at that offset are malformed
		data := make([]byte, 256)
		// At offset 224, set length to a huge number that exceeds data
		big.NewInt(9999).FillBytes(data[224:256])

		w := make([]byte, 32)
		big.NewInt(224).FillBytes(w)

		result := decodeSignatureData(data, w, 224)
		assert.Equal(t, "0x"+hex.EncodeToString(w), result)
	})
}

// ---------------------------------------------------------------------------
// finalizeEvent
// ---------------------------------------------------------------------------

func TestFinalizeEvent(t *testing.T) {
	logger := zerolog.New(nil).Level(zerolog.Disabled)

	t.Run("txType 0 sets FAST confirmation", func(t *testing.T) {
		event := &store.Event{}
		payload := &common.UniversalTx{TxType: 0, Sender: "0xabc"}
		finalizeEvent(event, payload, logger)

		assert.Equal(t, store.ConfirmationFast, event.ConfirmationType)
		assert.NotNil(t, event.EventData)

		var decoded common.UniversalTx
		err := json.Unmarshal(event.EventData, &decoded)
		require.NoError(t, err)
		assert.Equal(t, "0xabc", decoded.Sender)
	})

	t.Run("txType 1 sets FAST confirmation", func(t *testing.T) {
		event := &store.Event{}
		payload := &common.UniversalTx{TxType: 1}
		finalizeEvent(event, payload, logger)

		assert.Equal(t, store.ConfirmationFast, event.ConfirmationType)
	})

	t.Run("txType 2 sets STANDARD confirmation", func(t *testing.T) {
		event := &store.Event{}
		payload := &common.UniversalTx{TxType: 2}
		finalizeEvent(event, payload, logger)

		assert.Equal(t, store.ConfirmationStandard, event.ConfirmationType)
	})

	t.Run("txType 3 sets STANDARD confirmation", func(t *testing.T) {
		event := &store.Event{}
		payload := &common.UniversalTx{TxType: 3}
		finalizeEvent(event, payload, logger)

		assert.Equal(t, store.ConfirmationStandard, event.ConfirmationType)
	})

	t.Run("high txType sets STANDARD confirmation", func(t *testing.T) {
		event := &store.Event{}
		payload := &common.UniversalTx{TxType: 255}
		finalizeEvent(event, payload, logger)

		assert.Equal(t, store.ConfirmationStandard, event.ConfirmationType)
	})

	t.Run("event data is valid JSON", func(t *testing.T) {
		event := &store.Event{}
		payload := &common.UniversalTx{
			SourceChain: "eip155:1",
			Sender:      "0xsender",
			Recipient:   "0xrecipient",
			Token:       "0xtoken",
			Amount:      "1000",
			TxType:      0,
		}
		finalizeEvent(event, payload, logger)

		var decoded common.UniversalTx
		err := json.Unmarshal(event.EventData, &decoded)
		require.NoError(t, err)
		assert.Equal(t, "eip155:1", decoded.SourceChain)
		assert.Equal(t, "0xsender", decoded.Sender)
		assert.Equal(t, "0xrecipient", decoded.Recipient)
		assert.Equal(t, "0xtoken", decoded.Token)
		assert.Equal(t, "1000", decoded.Amount)
	})
}
