package evm

import (
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
			eventType: EventTypeOutboundObservation,
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
					eventSignature,    // Topics[0]: event signature
					txIDBytes,         // Topics[1]: txID (bytes32)
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
				assert.Equal(t, common.EventTypeOutbound, event.Type)
				assert.Equal(t, "PENDING", event.Status)
				assert.Equal(t, "STANDARD", event.ConfirmationType)

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
			event := ParseEvent(tt.log, EventTypeOutboundObservation, chainID, logger)

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

		event := ParseEvent(log, EventTypeOutboundObservation, config.Chain, logger)
		require.NotNil(t, event)

		assert.Equal(t, common.EventTypeOutbound, event.Type)
		assert.Equal(t, "STANDARD", event.ConfirmationType)
		assert.Equal(t, uint64(77777), event.BlockHeight)

		var outboundData map[string]interface{}
		err := json.Unmarshal(event.EventData, &outboundData)
		require.NoError(t, err)

		assert.Contains(t, outboundData["tx_id"], "0xaaaa")
		assert.Contains(t, outboundData["universal_tx_id"], "0xbbbb")
	})
}
