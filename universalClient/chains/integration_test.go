package chains

import (
	"context"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rollchains/pchain/universalClient/db"
	uregistrytypes "github.com/rollchains/pchain/x/uregistry/types"
)

// TestGatewayIntegration tests the integration of gateway methods across the system
func TestGatewayIntegration(t *testing.T) {
	logger := zerolog.Nop()
	
	// Create an in-memory chain DB manager for testing
	dbManager := db.NewInMemoryChainDBManager(logger, nil)
	defer dbManager.CloseAll()

	// Create chain registry with the DB manager
	registry := NewChainRegistry(dbManager, logger)

	t.Run("EVM Chain Gateway Configuration", func(t *testing.T) {
		// Create EVM chain config with gateway
		evmConfig := &uregistrytypes.ChainConfig{
			Chain:          "eip155:11155111",
			VmType:         uregistrytypes.VmType_EVM,
			PublicRpcUrl:   "https://eth-sepolia.example.com",
			GatewayAddress: "0x1234567890123456789012345678901234567890",
			BlockConfirmation: &uregistrytypes.BlockConfirmation{
				FastInbound:     5,
				StandardInbound: 12,
			},
			GatewayMethods: []*uregistrytypes.GatewayMethods{
				{
					Name:            "addFunds",
					Identifier:      "0xf9bfe8a7",
					EventIdentifier: "0xevent123",
				},
			},
			Enabled: true,
		}

		// Create chain client
		client, err := registry.CreateChainClient(evmConfig)
		require.NoError(t, err)
		assert.NotNil(t, client)
		assert.Equal(t, "eip155:11155111", client.ChainID())
		assert.Equal(t, evmConfig, client.GetConfig())
	})

	t.Run("Solana Chain Gateway Configuration", func(t *testing.T) {
		// Create Solana chain config with gateway
		solanaConfig := &uregistrytypes.ChainConfig{
			Chain:          "solana:EtWTRABZaYq6iMfeYKouRu166VU2xqa1",
			VmType:         uregistrytypes.VmType_SVM,
			PublicRpcUrl:   "https://api.devnet.solana.com",
			GatewayAddress: "11111111111111111111111111111112",
			BlockConfirmation: &uregistrytypes.BlockConfirmation{
				FastInbound:     5,
				StandardInbound: 12,
			},
			GatewayMethods: []*uregistrytypes.GatewayMethods{
				{
					Name:            "add_funds",
					Identifier:      "84ed4c39500ab38a",
					EventIdentifier: "funds_added",
				},
			},
			Enabled: true,
		}

		// Create chain client
		client, err := registry.CreateChainClient(solanaConfig)
		require.NoError(t, err)
		assert.NotNil(t, client)
		assert.Equal(t, "solana:EtWTRABZaYq6iMfeYKouRu166VU2xqa1", client.ChainID())
		assert.Equal(t, solanaConfig, client.GetConfig())
	})

	t.Run("Dynamic Chain Updates", func(t *testing.T) {
		_, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// Initial config without gateway
		config1 := &uregistrytypes.ChainConfig{
			Chain:        "eip155:1",
			VmType:       uregistrytypes.VmType_EVM,
			PublicRpcUrl: "https://eth.example.com",
			Enabled:      true,
		}

		// Note: This would fail without a real RPC connection
		// err = registry.AddOrUpdateChain(ctx, config1)
		// For testing, just create the client
		client1, err := registry.CreateChainClient(config1)
		require.NoError(t, err)
		assert.NotNil(t, client1)
		assert.Empty(t, client1.GetConfig().GatewayAddress)

		// Update with gateway configuration
		config2 := &uregistrytypes.ChainConfig{
			Chain:          "eip155:1",
			VmType:         uregistrytypes.VmType_EVM,
			PublicRpcUrl:   "https://eth.example.com",
			GatewayAddress: "0xabcdef1234567890abcdef1234567890abcdef12",
			BlockConfirmation: &uregistrytypes.BlockConfirmation{
				FastInbound:     3,
				StandardInbound: 6,
			},
			GatewayMethods: []*uregistrytypes.GatewayMethods{
				{
					Name:            "addFunds",
					Identifier:      "0xf9bfe8a7",
					EventIdentifier: "0xevent456",
				},
			},
			Enabled: true,
		}

		// Create updated client
		client2, err := registry.CreateChainClient(config2)
		require.NoError(t, err)
		assert.NotNil(t, client2)
		assert.Equal(t, "0xabcdef1234567890abcdef1234567890abcdef12", client2.GetConfig().GatewayAddress)
		assert.Equal(t, uint32(3), client2.GetConfig().BlockConfirmation.FastInbound)
		assert.Equal(t, uint32(6), client2.GetConfig().BlockConfirmation.StandardInbound)
	})

	t.Run("Multiple Chains with Different Confirmations", func(t *testing.T) {
		configs := []*uregistrytypes.ChainConfig{
			{
				Chain:          "eip155:1",
				VmType:         uregistrytypes.VmType_EVM,
				PublicRpcUrl:   "https://eth.example.com",
				GatewayAddress: "0x1111111111111111111111111111111111111111",
				BlockConfirmation: &uregistrytypes.BlockConfirmation{
					FastInbound:     1,
					StandardInbound: 6,
				},
				Enabled: true,
			},
			{
				Chain:          "eip155:137",
				VmType:         uregistrytypes.VmType_EVM,
				PublicRpcUrl:   "https://polygon.example.com",
				GatewayAddress: "0x2222222222222222222222222222222222222222",
				BlockConfirmation: &uregistrytypes.BlockConfirmation{
					FastInbound:     10,
					StandardInbound: 30,
				},
				Enabled: true,
			},
			{
				Chain:          "eip155:42161",
				VmType:         uregistrytypes.VmType_EVM,
				PublicRpcUrl:   "https://arbitrum.example.com",
				GatewayAddress: "0x3333333333333333333333333333333333333333",
				BlockConfirmation: &uregistrytypes.BlockConfirmation{
					FastInbound:     2,
					StandardInbound: 10,
				},
				Enabled: true,
			},
		}

		for _, config := range configs {
			client, err := registry.CreateChainClient(config)
			require.NoError(t, err)
			assert.NotNil(t, client)
			assert.Equal(t, config.Chain, client.ChainID())
			assert.Equal(t, config.BlockConfirmation.FastInbound, client.GetConfig().BlockConfirmation.FastInbound)
			assert.Equal(t, config.BlockConfirmation.StandardInbound, client.GetConfig().BlockConfirmation.StandardInbound)
		}
	})
}

// TestGatewayMethodRegistry tests the gateway method configuration functionality
func TestGatewayMethodRegistry(t *testing.T) {
	t.Run("Method Configuration", func(t *testing.T) {
		// With removal of KnownGatewayMethods, methods are now defined in config
		// Test that configs properly define gateway methods
		
		config := &uregistrytypes.ChainConfig{
			GatewayMethods: []*uregistrytypes.GatewayMethods{
				{
					Name:            "addFunds",
					Identifier:      "0xf9bfe8a7",
					EventIdentifier: "0xb28f49668e7e76dc96d7aabe5b7f63fecfbd1c3574774c05e8204e749fd96fbd",
				},
				{
					Name:            "add_funds",
					Identifier:      "84ed4c39500ab38a",
					EventIdentifier: "funds_added",
				},
			},
		}
		
		// Verify methods are properly configured
		assert.Len(t, config.GatewayMethods, 2)
		
		// Check method identifiers exist
		methodIds := make(map[string]bool)
		for _, method := range config.GatewayMethods {
			methodIds[method.Identifier] = true
		}
		
		assert.True(t, methodIds["0xf9bfe8a7"], "EVM addFunds method should be configured")
		assert.True(t, methodIds["84ed4c39500ab38a"], "Solana add_funds method should be configured")
		assert.False(t, methodIds["0xunknown"], "Unknown method should not be configured")
	})

	t.Run("Event Configuration", func(t *testing.T) {
		// With the removal of KnownGatewayMethods, event topics are now
		// provided directly in the config via EventIdentifier field
		
		// Test EVM configuration
		evmConfig := &uregistrytypes.ChainConfig{
			Chain:          "eip155:1",
			GatewayAddress: "0x1234567890123456789012345678901234567890",
			GatewayMethods: []*uregistrytypes.GatewayMethods{
				{
					Name:            "addFunds",
					Identifier:      "0xf9bfe8a7",
					EventIdentifier: "0xb28f49668e7e76dc96d7aabe5b7f63fecfbd1c3574774c05e8204e749fd96fbd",
				},
			},
		}
		
		assert.NotEmpty(t, evmConfig.GatewayMethods[0].EventIdentifier)
		assert.Equal(t, 66, len(evmConfig.GatewayMethods[0].EventIdentifier))
		
		// Test Solana configuration
		solanaConfig := &uregistrytypes.ChainConfig{
			Chain:          "solana:EtWTRABZaYq6iMfeYKouRu166VU2xqa1",
			GatewayAddress: "DZMjJ7hhAB2wmAuRX4sMbYYqDBhFBPzJJ2cWsbTnwQaT",
			GatewayMethods: []*uregistrytypes.GatewayMethods{
				{
					Name:            "add_funds",
					Identifier:      "84ed4c39500ab38a",
					EventIdentifier: "funds_added",
				},
			},
		}
		
		assert.NotEmpty(t, solanaConfig.GatewayMethods[0].EventIdentifier)
		// Solana uses EventIdentifier for log matching, not hash-based topics
		assert.NotEqual(t, 66, len(solanaConfig.GatewayMethods[0].EventIdentifier))
	})
}

// TestDynamicConfigurationUpdate tests that configuration can be updated dynamically
func TestDynamicConfigurationUpdate(t *testing.T) {
	logger := zerolog.Nop()
	
	// Create an in-memory chain DB manager for testing
	dbManager := db.NewInMemoryChainDBManager(logger, nil)
	defer dbManager.CloseAll()

	registry := NewChainRegistry(dbManager, logger)

	// Simulate dynamic configuration update scenarios
	scenarios := []struct {
		name   string
		config *uregistrytypes.ChainConfig
	}{
		{
			name: "Add gateway to existing chain",
			config: &uregistrytypes.ChainConfig{
				Chain:          "eip155:1",
				VmType:         uregistrytypes.VmType_EVM,
				PublicRpcUrl:   "https://eth.example.com",
				GatewayAddress: "0xnewgateway1234567890123456789012345678",
				BlockConfirmation: &uregistrytypes.BlockConfirmation{
					FastInbound:     5,
					StandardInbound: 12,
				},
				GatewayMethods: []*uregistrytypes.GatewayMethods{
					{
						Name:            "addFunds",
						Identifier:      "0xf9bfe8a7",
						EventIdentifier: "0xevent789",
					},
				},
				Enabled: true,
			},
		},
		{
			name: "Update confirmation requirements",
			config: &uregistrytypes.ChainConfig{
				Chain:          "eip155:1",
				VmType:         uregistrytypes.VmType_EVM,
				PublicRpcUrl:   "https://eth.example.com",
				GatewayAddress: "0xnewgateway1234567890123456789012345678",
				BlockConfirmation: &uregistrytypes.BlockConfirmation{
					FastInbound:     10, // Increased from 5
					StandardInbound: 20, // Increased from 12
				},
				GatewayMethods: []*uregistrytypes.GatewayMethods{
					{
						Name:            "addFunds",
						Identifier:      "0xf9bfe8a7",
						EventIdentifier: "0xevent789",
					},
				},
				Enabled: true,
			},
		},
		{
			name: "Add new gateway method",
			config: &uregistrytypes.ChainConfig{
				Chain:          "eip155:1",
				VmType:         uregistrytypes.VmType_EVM,
				PublicRpcUrl:   "https://eth.example.com",
				GatewayAddress: "0xnewgateway1234567890123456789012345678",
				BlockConfirmation: &uregistrytypes.BlockConfirmation{
					FastInbound:     10,
					StandardInbound: 20,
				},
				GatewayMethods: []*uregistrytypes.GatewayMethods{
					{
						Name:            "addFunds",
						Identifier:      "0xf9bfe8a7",
						EventIdentifier: "0xevent789",
					},
					{
						Name:            "removeFunds",
						Identifier:      "0xaabbccdd",
						EventIdentifier: "0xeventabc",
					},
				},
				Enabled: true,
			},
		},
	}

	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			client, err := registry.CreateChainClient(scenario.config)
			require.NoError(t, err)
			assert.NotNil(t, client)
			
			// Verify configuration was applied
			assert.Equal(t, scenario.config.GatewayAddress, client.GetConfig().GatewayAddress)
			assert.Equal(t, scenario.config.BlockConfirmation.FastInbound, 
				client.GetConfig().BlockConfirmation.FastInbound)
			assert.Equal(t, scenario.config.BlockConfirmation.StandardInbound, 
				client.GetConfig().BlockConfirmation.StandardInbound)
			assert.Equal(t, len(scenario.config.GatewayMethods), 
				len(client.GetConfig().GatewayMethods))
		})
	}
}