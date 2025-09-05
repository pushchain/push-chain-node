package evm

import (
	"math/big"
	"testing"

	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
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
				Chain:          "eip155:1",
				GatewayAddress: "0x742d35Cc6634C0532925a3b844Bc9e7595f0bEb7",
				GatewayMethods: []*uregistrytypes.GatewayMethods{
					{
						Name:            "addFunds",
						Identifier:      "method1",
						EventIdentifier: "0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
					},
					{
						Name:            "withdrawFunds",
						Identifier:      "method2",
						EventIdentifier: "0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
					},
				},
			},
			wantTopics: 2,
		},
		{
			name: "handles methods without event identifiers",
			config: &uregistrytypes.ChainConfig{
				Chain:          "eip155:1",
				GatewayAddress: "0x742d35Cc6634C0532925a3b844Bc9e7595f0bEb7",
				GatewayMethods: []*uregistrytypes.GatewayMethods{
					{
						Name:            "addFunds",
						Identifier:      "method1",
						EventIdentifier: "0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
					},
					{
						Name:       "noEvent",
						Identifier: "method2",
						// No EventIdentifier
					},
				},
			},
			wantTopics: 1,
		},
		{
			name: "handles empty gateway methods",
			config: &uregistrytypes.ChainConfig{
				Chain:          "eip155:1",
				GatewayAddress: "0x742d35Cc6634C0532925a3b844Bc9e7595f0bEb7",
				GatewayMethods: []*uregistrytypes.GatewayMethods{},
			},
			wantTopics: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := zerolog.New(nil).Level(zerolog.Disabled)
			gatewayAddr := ethcommon.HexToAddress(tt.config.GatewayAddress)

			parser := NewEventParser(gatewayAddr, tt.config, logger)

			require.NotNil(t, parser)
			assert.Equal(t, tt.wantTopics, len(parser.eventTopics))
			assert.Equal(t, gatewayAddr, parser.gatewayAddr)
			assert.Equal(t, tt.config, parser.config)
		})
	}
}

func TestParseGatewayEvent(t *testing.T) {
	gatewayAddr := ethcommon.HexToAddress("0x742d35Cc6634C0532925a3b844Bc9e7595f0bEb7")
	eventTopic := ethcommon.HexToHash("0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef")

	config := &uregistrytypes.ChainConfig{
		Chain:          "eip155:1",
		GatewayAddress: gatewayAddr.Hex(),
		GatewayMethods: []*uregistrytypes.GatewayMethods{
			{
				Name:            "addFunds",
				Identifier:      "method1",
				EventIdentifier: eventTopic.Hex(),
			},
		},
	}

	logger := zerolog.New(nil).Level(zerolog.Disabled)
	parser := NewEventParser(gatewayAddr, config, logger)

	tests := []struct {
		name      string
		log       *types.Log
		wantEvent bool
		validate  func(*testing.T, *common.GatewayEvent)
	}{
		{
			name: "parses valid gateway event",
			log: &types.Log{
				Address:     gatewayAddr,
				Topics:      []ethcommon.Hash{eventTopic},
				Data:        []byte{},
				TxHash:      ethcommon.HexToHash("0xabc123"),
				BlockNumber: 12345,
			},
			wantEvent: true,
			validate: func(t *testing.T, event *common.GatewayEvent) {
				assert.Equal(t, "eip155:1", event.ChainID)
				assert.Equal(t, "0x0000000000000000000000000000000000000000000000000000000000abc123", event.TxHash)
				assert.Equal(t, uint64(12345), event.BlockNumber)
				assert.Equal(t, "addFunds", event.Method)
				assert.Equal(t, "method1", event.EventID)
			},
		},
		{
			name: "parses addFunds event with amount",
			log: &types.Log{
				Address: gatewayAddr,
				Topics: []ethcommon.Hash{
					eventTopic,
					ethcommon.HexToHash("0x000000000000000000000000742d35cc6634c0532925a3b844bc9e7595f0beb7"), // sender
					ethcommon.HexToHash("0x000000000000000000000000dac17f958d2ee523a2206206994597c13d831ec7"), // token/receiver
				},
				Data: func() []byte {
					data := make([]byte, 32)
					big.NewInt(1000000).FillBytes(data)
					return data
				}(), // amount as 32 bytes
				TxHash:      ethcommon.HexToHash("0xdef456"),
				BlockNumber: 12346,
			},
			wantEvent: true,
			validate: func(t *testing.T, event *common.GatewayEvent) {
				assert.Equal(t, "addFunds", event.Method)
				assert.Equal(t, "0x742D35Cc6634C0532925A3B844bC9e7595f0bEB7", event.Sender)
				assert.Equal(t, "0xdAC17F958D2ee523a2206206994597C13D831ec7", event.Receiver)
				assert.Equal(t, "1000000", event.Amount)
			},
		},
		{
			name: "returns nil for log with no topics",
			log: &types.Log{
				Address: gatewayAddr,
				Topics:  []ethcommon.Hash{},
				Data:    []byte{},
			},
			wantEvent: false,
		},
		{
			name: "returns nil for unknown event topic",
			log: &types.Log{
				Address: gatewayAddr,
				Topics:  []ethcommon.Hash{ethcommon.HexToHash("0xunknown")},
				Data:    []byte{},
			},
			wantEvent: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := parser.ParseGatewayEvent(tt.log)

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
		Chain:          "eip155:1",
		GatewayAddress: "0x742d35Cc6634C0532925a3b844Bc9e7595f0bEb7",
		GatewayMethods: []*uregistrytypes.GatewayMethods{
			{
				Name:            "method1",
				Identifier:      "id1",
				EventIdentifier: "0x1111111111111111111111111111111111111111111111111111111111111111",
			},
			{
				Name:            "method2",
				Identifier:      "id2",
				EventIdentifier: "0x2222222222222222222222222222222222222222222222222222222222222222",
			},
		},
	}

	logger := zerolog.New(nil).Level(zerolog.Disabled)
	parser := NewEventParser(ethcommon.Address{}, config, logger)

	topics := parser.GetEventTopics()
	assert.Len(t, topics, 2)

	// Check that both topics are present (order doesn't matter)
	topicSet := make(map[ethcommon.Hash]bool)
	for _, topic := range topics {
		topicSet[topic] = true
	}

	assert.True(t, topicSet[ethcommon.HexToHash("0x1111111111111111111111111111111111111111111111111111111111111111")])
	assert.True(t, topicSet[ethcommon.HexToHash("0x2222222222222222222222222222222222222222222222222222222222222222")])
}

func TestHasEvents(t *testing.T) {
	tests := []struct {
		name     string
		config   *uregistrytypes.ChainConfig
		expected bool
	}{
		{
			name: "returns true when events are configured",
			config: &uregistrytypes.ChainConfig{
				GatewayMethods: []*uregistrytypes.GatewayMethods{
					{
						EventIdentifier: "0x1234",
						Identifier:      "id1",
					},
				},
			},
			expected: true,
		},
		{
			name: "returns false when no events are configured",
			config: &uregistrytypes.ChainConfig{
				GatewayMethods: []*uregistrytypes.GatewayMethods{
					{
						Identifier: "id1",
						// No EventIdentifier
					},
				},
			},
			expected: false,
		},
		{
			name: "returns false for empty config",
			config: &uregistrytypes.ChainConfig{
				GatewayMethods: []*uregistrytypes.GatewayMethods{},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := zerolog.New(nil).Level(zerolog.Disabled)
			parser := NewEventParser(ethcommon.Address{}, tt.config, logger)
			assert.Equal(t, tt.expected, parser.HasEvents())
		})
	}
}

func TestParseEventData(t *testing.T) {
	config := &uregistrytypes.ChainConfig{
		Chain: "eip155:1",
		GatewayMethods: []*uregistrytypes.GatewayMethods{
			{
				Name:            "addFunds",
				Identifier:      "method1",
				EventIdentifier: "0x1234",
			},
		},
	}

	logger := zerolog.New(nil).Level(zerolog.Disabled)
	parser := NewEventParser(ethcommon.Address{}, config, logger)

	t.Run("parses addFunds event data correctly", func(t *testing.T) {
		// Create amount data (32 bytes for uint256)
		amount := big.NewInt(1000000)
		amountBytes := make([]byte, 32)
		amount.FillBytes(amountBytes)

		log := &types.Log{
			Topics: []ethcommon.Hash{
				ethcommon.HexToHash("0x1234"), // event signature
				ethcommon.HexToHash("0x000000000000000000000000742d35cc6634c0532925a3b844bc9e7595f0beb7"), // sender
				ethcommon.HexToHash("0x000000000000000000000000dac17f958d2ee523a2206206994597c13d831ec7"), // receiver
			},
			Data: amountBytes,
		}

		event := &common.GatewayEvent{}
		parser.parseEventData(event, log, "addFunds")

		assert.Equal(t, "0x742D35Cc6634C0532925A3B844bC9e7595f0bEB7", event.Sender)
		assert.Equal(t, "0xdAC17F958D2ee523a2206206994597C13D831ec7", event.Receiver)
		assert.Equal(t, "1000000", event.Amount)
	})

	t.Run("handles missing data gracefully", func(t *testing.T) {
		log := &types.Log{
			Topics: []ethcommon.Hash{
				ethcommon.HexToHash("0x1234"),
			},
			Data: []byte{}, // Empty data
		}

		event := &common.GatewayEvent{}
		parser.parseEventData(event, log, "addFunds")

		// Should not panic and should leave amount empty
		assert.Empty(t, event.Amount)
	})
}