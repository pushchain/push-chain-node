package registry

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	
	uregistrytypes "github.com/rollchains/pchain/x/uregistry/types"
)

// TestRegistryClientIntegration tests the registry client against a local Push Chain
// Run this test with: go test -v ./universalClient/registry -run TestRegistryClientIntegration
func TestRegistryClientIntegration(t *testing.T) {
	// Skip if not explicitly enabled
	if testing.Short() {
		t.Skip("Skipping integration test in short mode. Run with: go test -v")
	}

	// Setup
	logger := zerolog.New(zerolog.ConsoleWriter{Out: os.Stdout}).With().Timestamp().Logger()
	
	// Use localhost:9090 as the default gRPC endpoint for local Push Chain
	grpcURLs := []string{"localhost:9090"}
	
	// Create registry client
	client, err := NewRegistryClient(grpcURLs, logger)
	require.NoError(t, err, "Failed to create registry client")
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	t.Run("TestHealthCheck", func(t *testing.T) {
		// Health check is now done internally during construction
		// If we got here, at least one connection is healthy
		assert.NotNil(t, client, "Registry client should be created successfully")
	})

	t.Run("TestGetAllChainConfigs", func(t *testing.T) {
		configs, err := client.GetAllChainConfigs(ctx)
		require.NoError(t, err, "Failed to get all chain configs")
		require.NotEmpty(t, configs, "Should have at least one chain config")

		// Log the chains we found
		for _, config := range configs {
			t.Logf("Found chain: %s (VM Type: %v, Enabled: %v)", 
				config.Chain, config.VmType, config.Enabled)
			t.Logf("  RPC URL: %s", config.PublicRpcUrl)
			t.Logf("  Gateway: %s", config.GatewayAddress)
		}
	})

	t.Run("TestGetSpecificChainConfigs", func(t *testing.T) {
		// Test EVM chain
		evmChain := "eip155:11155111"
		evmConfig, err := client.GetChainConfig(ctx, evmChain)
		require.NoError(t, err, "Failed to get EVM chain config")
		require.NotNil(t, evmConfig, "EVM chain config should not be nil")
		assert.Equal(t, evmChain, evmConfig.Chain)
		t.Logf("EVM Chain Config: %+v", evmConfig)

		// Test Solana chain - handle gracefully if not found
		solanaChain := "solana:EtWTRABZaYq6iMfeYKouRu166VU2xqa1"
		solanaConfig, err := client.GetChainConfig(ctx, solanaChain)
		if err != nil {
			t.Logf("Solana chain config not found (this is expected if not configured): %v", err)
		} else {
			require.NotNil(t, solanaConfig, "Solana chain config should not be nil")
			assert.Equal(t, solanaChain, solanaConfig.Chain)
			t.Logf("Solana Chain Config: %+v", solanaConfig)
		}
	})

	t.Run("TestGetTokenConfigsByChain", func(t *testing.T) {
		// Test tokens on EVM chain
		evmChain := "eip155:11155111"
		evmTokens, err := client.GetTokenConfigsByChain(ctx, evmChain)
		require.NoError(t, err, "Failed to get EVM token configs")
		t.Logf("Found %d tokens on %s", len(evmTokens), evmChain)
		
		for _, token := range evmTokens {
			t.Logf("  Token: %s (%s) at %s", token.Name, token.Symbol, token.Address)
		}

		// Test tokens on Solana chain - handle gracefully if chain not found
		solanaChain := "solana:EtWTRABZaYq6iMfeYKouRu166VU2xqa1"
		solanaTokens, err := client.GetTokenConfigsByChain(ctx, solanaChain)
		if err != nil {
			t.Logf("Solana token configs not found (this is expected if chain not configured): %v", err)
		} else {
			t.Logf("Found %d tokens on %s", len(solanaTokens), solanaChain)
			
			for _, token := range solanaTokens {
				t.Logf("  Token: %s (%s) at %s", token.Name, token.Symbol, token.Address)
			}
		}
	})

	t.Run("TestGetAllTokenConfigs", func(t *testing.T) {
		tokens, err := client.GetAllTokenConfigs(ctx)
		require.NoError(t, err, "Failed to get all token configs")
		t.Logf("Found %d total tokens across all chains", len(tokens))
		
		// Group by chain for summary
		tokensByChain := make(map[string]int)
		for _, token := range tokens {
			tokensByChain[token.Chain]++
		}
		
		for chain, count := range tokensByChain {
			t.Logf("  %s: %d tokens", chain, count)
		}
	})

	t.Run("TestRetryLogic", func(t *testing.T) {
		// Test with an invalid chain ID to trigger a not found error
		_, err := client.GetChainConfig(ctx, "invalid:chain")
		assert.Error(t, err, "Should get error for invalid chain")
		t.Logf("Expected error: %v", err)
	})
}

// TestCacheIntegration tests the cache functionality
func TestCacheIntegration(t *testing.T) {
	logger := zerolog.New(zerolog.ConsoleWriter{Out: os.Stdout}).With().Timestamp().Logger()
	cache := NewConfigCache(logger)

	// Mock chain configs
	chainConfigs := []*uregistrytypes.ChainConfig{
		{
			Chain:          "eip155:11155111",
			VmType:         uregistrytypes.VmType_EVM,
			PublicRpcUrl:   "https://eth-sepolia.example.com",
			GatewayAddress: "0x123...",
			Enabled:        true,
		},
		{
			Chain:          "solana:EtWTRABZaYq6iMfeYKouRu166VU2xqa1",
			VmType:         uregistrytypes.VmType_SVM,
			PublicRpcUrl:   "https://api.devnet.solana.com",
			GatewayAddress: "Sol123...",
			Enabled:        true,
		},
	}

	// Mock token configs
	tokenConfigs := []*uregistrytypes.TokenConfig{
		{
			Chain:   "eip155:11155111",
			Address: "0xAAA...",
			Name:    "Test Token",
			Symbol:  "TEST",
		},
		{
			Chain:   "eip155:11155111",
			Address: "0xBBB...",
			Name:    "Another Token",
			Symbol:  "ATK",
		},
		{
			Chain:   "solana:EtWTRABZaYq6iMfeYKouRu166VU2xqa1",
			Address: "SolToken...",
			Name:    "Solana Test",
			Symbol:  "SOL",
		},
	}

	t.Run("TestCacheOperations", func(t *testing.T) {
		// Update cache
		cache.UpdateAll(chainConfigs, tokenConfigs)

		// Test chain retrieval
		evmChain := cache.GetChainConfig("eip155:11155111")
		assert.NotNil(t, evmChain)
		assert.Equal(t, "eip155:11155111", evmChain.Chain)

		// Test all chains
		allChains := cache.GetAllChainConfigs()
		assert.Len(t, allChains, 2)

		// Test token retrieval
		token := cache.GetTokenConfig("eip155:11155111", "0xAAA...")
		assert.NotNil(t, token)
		assert.Equal(t, "TEST", token.Symbol)

		// Test tokens by chain
		evmTokens := cache.GetTokenConfigsByChain("eip155:11155111")
		assert.Len(t, evmTokens, 2)

		solanaTokens := cache.GetTokenConfigsByChain("solana:EtWTRABZaYq6iMfeYKouRu166VU2xqa1")
		assert.Len(t, solanaTokens, 1)

		// Test all tokens
		allTokens := cache.GetAllTokenConfigs()
		assert.Len(t, allTokens, 3)
	})

}