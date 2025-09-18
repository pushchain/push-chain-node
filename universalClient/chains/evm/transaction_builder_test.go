package evm

import (
	"testing"

	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
)

func TestNewTransactionBuilder(t *testing.T) {
	logger := zerolog.New(nil).Level(zerolog.Disabled)
	gatewayAddr := ethcommon.HexToAddress("0x1234567890123456789012345678901234567890")
	client := &Client{}

	builder := NewTransactionBuilder(client, gatewayAddr, logger)

	assert.NotNil(t, builder)
	assert.Equal(t, client, builder.parentClient)
	assert.Equal(t, gatewayAddr, builder.gatewayAddr)
}

func TestTransactionBuilder_Fields(t *testing.T) {
	tests := []struct {
		name        string
		gatewayAddr string
	}{
		{
			name:        "standard address",
			gatewayAddr: "0x1234567890123456789012345678901234567890",
		},
		{
			name:        "zero address",
			gatewayAddr: "0x0000000000000000000000000000000000000000",
		},
		{
			name:        "max address",
			gatewayAddr: "0xFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := zerolog.New(nil).Level(zerolog.Disabled)
			gatewayAddr := ethcommon.HexToAddress(tt.gatewayAddr)
			client := &Client{}

			builder := NewTransactionBuilder(client, gatewayAddr, logger)

			assert.NotNil(t, builder)
			assert.Equal(t, client, builder.parentClient)
			assert.Equal(t, gatewayAddr, builder.gatewayAddr)
			assert.NotNil(t, builder.logger)
		})
	}
}

func TestTransactionBuilder_LoggerComponent(t *testing.T) {
	logger := zerolog.New(nil).Level(zerolog.Disabled)
	gatewayAddr := ethcommon.HexToAddress("0x1234567890123456789012345678901234567890")
	client := &Client{}

	builder := NewTransactionBuilder(client, gatewayAddr, logger)

	// Verify the logger was properly initialized with the component name
	assert.NotNil(t, builder.logger)
	// The logger should have the component set to "evm_tx_builder"
	// This is set in the NewTransactionBuilder function
}

func TestTransactionBuilder_ClientReference(t *testing.T) {
	logger := zerolog.New(nil).Level(zerolog.Disabled)
	gatewayAddr := ethcommon.HexToAddress("0x1234567890123456789012345678901234567890")

	// Test with different client states
	t.Run("with initialized client", func(t *testing.T) {
		client := &Client{
			chainID: 1,
		}

		builder := NewTransactionBuilder(client, gatewayAddr, logger)

		assert.NotNil(t, builder)
		assert.Equal(t, client, builder.parentClient)
		assert.Equal(t, int64(1), builder.parentClient.chainID)
	})

	t.Run("with nil client fields", func(t *testing.T) {
		client := &Client{}

		builder := NewTransactionBuilder(client, gatewayAddr, logger)

		assert.NotNil(t, builder)
		assert.Equal(t, client, builder.parentClient)
	})
}

func TestTransactionBuilder_AddressValidation(t *testing.T) {
	tests := []struct {
		name    string
		address string
	}{
		{
			name:    "checksummed address",
			address: "0x5aAeb6053f3E94C9b9A09f33669435E7Ef1BeAed",
		},
		{
			name:    "lowercase address",
			address: "0x5aaeb6053f3e94c9b9a09f33669435e7ef1beaed",
		},
		{
			name:    "uppercase address",
			address: "0x5AAEB6053F3E94C9B9A09F33669435E7EF1BEAED",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := zerolog.New(nil).Level(zerolog.Disabled)
			gatewayAddr := ethcommon.HexToAddress(tt.address)
			client := &Client{}

			builder := NewTransactionBuilder(client, gatewayAddr, logger)

			assert.NotNil(t, builder)
			// All addresses should be stored in the same format internally
			assert.Equal(t, gatewayAddr, builder.gatewayAddr)

			// Verify the address is a valid 20-byte Ethereum address
			assert.Equal(t, 20, len(builder.gatewayAddr.Bytes()))
		})
	}
}

func TestTransactionBuilder_Initialization(t *testing.T) {
	// Test that all fields are properly initialized
	logger := zerolog.New(nil).Level(zerolog.Disabled)
	gatewayAddr := ethcommon.HexToAddress("0x742d35Cc6634C0532925a3b844Bc9e7595f0bEb")
	client := &Client{
		chainID: 137, // Polygon
	}

	builder := NewTransactionBuilder(client, gatewayAddr, logger)

	// Verify initialization
	assert.NotNil(t, builder, "Builder should not be nil")
	assert.NotNil(t, builder.parentClient, "Parent client should not be nil")
	assert.NotNil(t, builder.logger, "Logger should not be nil")

	// Verify the correct references are stored
	assert.Same(t, client, builder.parentClient, "Should store the same client reference")
	assert.Equal(t, gatewayAddr, builder.gatewayAddr, "Should store the gateway address")
}

func TestTransactionBuilder_MultipleInstances(t *testing.T) {
	// Test creating multiple builders with different configurations
	logger := zerolog.New(nil).Level(zerolog.Disabled)

	client1 := &Client{chainID: 1}
	client2 := &Client{chainID: 137}

	addr1 := ethcommon.HexToAddress("0x1111111111111111111111111111111111111111")
	addr2 := ethcommon.HexToAddress("0x2222222222222222222222222222222222222222")

	builder1 := NewTransactionBuilder(client1, addr1, logger)
	builder2 := NewTransactionBuilder(client2, addr2, logger)

	// Verify each builder maintains its own configuration
	assert.NotEqual(t, builder1.parentClient, builder2.parentClient)
	assert.NotEqual(t, builder1.gatewayAddr, builder2.gatewayAddr)

	assert.Equal(t, client1, builder1.parentClient)
	assert.Equal(t, client2, builder2.parentClient)
	assert.Equal(t, addr1, builder1.gatewayAddr)
	assert.Equal(t, addr2, builder2.gatewayAddr)
}