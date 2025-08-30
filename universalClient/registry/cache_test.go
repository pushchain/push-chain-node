package registry

import (
	"sync"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	uregistrytypes "github.com/rollchains/pchain/x/uregistry/types"
)

// TestConfigCacheBasicOperations tests basic cache operations
func TestConfigCacheBasicOperations(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))
	cache := NewConfigCache(logger)

	// Test data
	chainConfigs := []*uregistrytypes.ChainConfig{
		{
			Chain:          "eip155:11155111",
			VmType:         uregistrytypes.VmType_EVM,
			PublicRpcUrl:   "https://eth-sepolia.example.com",
			GatewayAddress: "0x123...",
			Enabled:        &uregistrytypes.ChainEnabled{IsInboundEnabled: true, IsOutboundEnabled: true},
		},
		{
			Chain:          "solana:EtWTRABZaYq6iMfeYKouRu166VU2xqa1",
			VmType:         uregistrytypes.VmType_SVM,
			PublicRpcUrl:   "https://api.devnet.solana.com",
			GatewayAddress: "Sol123...",
			Enabled:        &uregistrytypes.ChainEnabled{IsInboundEnabled: true, IsOutboundEnabled: true},
		},
	}

	tokenConfigs := []*uregistrytypes.TokenConfig{
		{
			Chain:   "eip155:11155111",
			Address: "0xAAA...",
			Name:    "Test Token A",
			Symbol:  "TKA",
		},
		{
			Chain:   "eip155:11155111",
			Address: "0xBBB...",
			Name:    "Test Token B",
			Symbol:  "TKB",
		},
		{
			Chain:   "solana:EtWTRABZaYq6iMfeYKouRu166VU2xqa1",
			Address: "SolToken...",
			Name:    "Solana Token",
			Symbol:  "SOLT",
		},
	}

	t.Run("GetChainConfig_EmptyCache", func(t *testing.T) {
		config := cache.GetChainConfig("eip155:11155111")
		assert.Nil(t, config)
	})

	t.Run("GetTokenConfig_EmptyCache", func(t *testing.T) {
		token := cache.GetTokenConfig("eip155:11155111", "0xAAA...")
		assert.Nil(t, token)
	})

	t.Run("UpdateAll", func(t *testing.T) {
		cache.UpdateAll(chainConfigs, tokenConfigs)

		// Verify chains
		chains := cache.GetAllChainConfigs()
		assert.Len(t, chains, 2)

		// Verify tokens
		tokens := cache.GetAllTokenConfigs()
		assert.Len(t, tokens, 3)
	})

	t.Run("GetChainConfig", func(t *testing.T) {
		config := cache.GetChainConfig("eip155:11155111")
		require.NotNil(t, config)
		assert.Equal(t, "eip155:11155111", config.Chain)
		assert.Equal(t, uregistrytypes.VmType_EVM, config.VmType)

		// Non-existent chain
		config = cache.GetChainConfig("invalid:chain")
		assert.Nil(t, config)
	})

	t.Run("GetTokenConfig", func(t *testing.T) {
		token := cache.GetTokenConfig("eip155:11155111", "0xAAA...")
		require.NotNil(t, token)
		assert.Equal(t, "Test Token A", token.Name)
		assert.Equal(t, "TKA", token.Symbol)

		// Non-existent token
		token = cache.GetTokenConfig("eip155:11155111", "0xZZZ...")
		assert.Nil(t, token)

		// Non-existent chain
		token = cache.GetTokenConfig("invalid:chain", "0xAAA...")
		assert.Nil(t, token)
	})

	t.Run("GetTokenConfigsByChain", func(t *testing.T) {
		// EVM chain with 2 tokens
		tokens := cache.GetTokenConfigsByChain("eip155:11155111")
		assert.Len(t, tokens, 2)

		// Solana chain with 1 token
		tokens = cache.GetTokenConfigsByChain("solana:EtWTRABZaYq6iMfeYKouRu166VU2xqa1")
		assert.Len(t, tokens, 1)

		// Non-existent chain
		tokens = cache.GetTokenConfigsByChain("invalid:chain")
		assert.Len(t, tokens, 0)
	})

	t.Run("UpdateChainConfigs_PreservesTokens", func(t *testing.T) {
		// UpdateChainConfigs should preserve tokens for existing chains
		// First, verify we have tokens
		tokens := cache.GetTokenConfigsByChain("eip155:11155111")
		assert.Len(t, tokens, 2)

		// Update chain config
		updatedChain := &uregistrytypes.ChainConfig{
			Chain:          "eip155:11155111",
			VmType:         uregistrytypes.VmType_EVM,
			PublicRpcUrl:   "https://eth-sepolia-new.example.com",
			GatewayAddress: "0x999...",
			Enabled:        &uregistrytypes.ChainEnabled{IsInboundEnabled: true, IsOutboundEnabled: true},
		}

		cache.UpdateChainConfigs([]*uregistrytypes.ChainConfig{updatedChain})
		
		// Verify chain was updated
		config := cache.GetChainConfig("eip155:11155111")
		require.NotNil(t, config)
		assert.Equal(t, "https://eth-sepolia-new.example.com", config.PublicRpcUrl)

		// Verify tokens were preserved
		tokens = cache.GetTokenConfigsByChain("eip155:11155111")
		assert.Len(t, tokens, 2)

		// Add a new chain
		newChain := &uregistrytypes.ChainConfig{
			Chain:          "eip155:1",
			VmType:         uregistrytypes.VmType_EVM,
			PublicRpcUrl:   "https://eth-mainnet.example.com",
			GatewayAddress: "0x456...",
			Enabled:        &uregistrytypes.ChainEnabled{IsInboundEnabled: true, IsOutboundEnabled: true},
		}

		cache.UpdateChainConfigs([]*uregistrytypes.ChainConfig{updatedChain, newChain})
		
		// Verify new chain exists
		config = cache.GetChainConfig("eip155:1")
		require.NotNil(t, config)
		assert.Equal(t, "eip155:1", config.Chain)

		// Verify old chain still exists with tokens
		tokens = cache.GetTokenConfigsByChain("eip155:11155111")
		assert.Len(t, tokens, 2)

		// Verify Solana chain is gone (UpdateChainConfigs replaces all chains)
		config = cache.GetChainConfig("solana:EtWTRABZaYq6iMfeYKouRu166VU2xqa1")
		assert.Nil(t, config)
		
		// Restore original data for next tests
		cache.UpdateAll(chainConfigs, tokenConfigs)
	})

	t.Run("UpdateTokenConfigs_Replaces", func(t *testing.T) {
		// UpdateTokenConfigs replaces all tokens (not merge)
		newToken := &uregistrytypes.TokenConfig{
			Chain:   "eip155:1",
			Address: "0xCCC...",
			Name:    "New Token",
			Symbol:  "NEW",
		}

		cache.UpdateTokenConfigs([]*uregistrytypes.TokenConfig{newToken})
		
		// Verify new token exists
		token := cache.GetTokenConfig("eip155:1", "0xCCC...")
		require.NotNil(t, token)
		assert.Equal(t, "New Token", token.Name)

		// Verify old tokens are gone (UpdateTokenConfigs replaces, not merges)
		token = cache.GetTokenConfig("eip155:11155111", "0xAAA...")
		assert.Nil(t, token)
	})

	t.Run("GetChainData", func(t *testing.T) {
		// Restore data
		cache.UpdateAll(chainConfigs, tokenConfigs)

		// Get chain data for EVM chain
		chainData := cache.GetChainData("eip155:11155111")
		require.NotNil(t, chainData)
		assert.NotNil(t, chainData.Config)
		assert.Equal(t, "eip155:11155111", chainData.Config.Chain)
		assert.Len(t, chainData.Tokens, 2)
		assert.NotNil(t, chainData.Tokens["0xAAA..."])
		assert.NotNil(t, chainData.Tokens["0xBBB..."])

		// Get chain data for Solana chain
		chainData = cache.GetChainData("solana:EtWTRABZaYq6iMfeYKouRu166VU2xqa1")
		require.NotNil(t, chainData)
		assert.NotNil(t, chainData.Config)
		assert.Equal(t, "solana:EtWTRABZaYq6iMfeYKouRu166VU2xqa1", chainData.Config.Chain)
		assert.Len(t, chainData.Tokens, 1)
		assert.NotNil(t, chainData.Tokens["SolToken..."])

		// Non-existent chain
		chainData = cache.GetChainData("invalid:chain")
		assert.Nil(t, chainData)
	})
}


// TestConfigCacheConcurrency tests thread-safe operations
func TestConfigCacheConcurrency(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))
	cache := NewConfigCache(logger)

	// Initial data
	chainConfigs := []*uregistrytypes.ChainConfig{
		{
			Chain:   "eip155:11155111",
			VmType:  uregistrytypes.VmType_EVM,
			Enabled: &uregistrytypes.ChainEnabled{IsInboundEnabled: true, IsOutboundEnabled: true},
		},
	}

	tokenConfigs := []*uregistrytypes.TokenConfig{
		{
			Chain:   "eip155:11155111",
			Address: "0xAAA...",
			Name:    "Test Token",
			Symbol:  "TEST",
		},
	}

	cache.UpdateAll(chainConfigs, tokenConfigs)

	// Test concurrent reads and writes
	var wg sync.WaitGroup
	iterations := 100

	// Concurrent readers
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				// Read operations
				_ = cache.GetChainConfig("eip155:11155111")
				_ = cache.GetTokenConfig("eip155:11155111", "0xAAA...")
				_ = cache.GetAllChainConfigs()
				_ = cache.GetAllTokenConfigs()
				_ = cache.GetTokenConfigsByChain("eip155:11155111")
			}
		}()
	}

	// Concurrent writers
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				// Write operations
				newChain := &uregistrytypes.ChainConfig{
					Chain:   "test:chain" + string(rune(id)),
					VmType:  uregistrytypes.VmType_EVM,
					Enabled: &uregistrytypes.ChainEnabled{IsInboundEnabled: true, IsOutboundEnabled: true},
				}
				cache.UpdateChainConfigs([]*uregistrytypes.ChainConfig{newChain})

				newToken := &uregistrytypes.TokenConfig{
					Chain:   "test:chain" + string(rune(id)),
					Address: "0x" + string(rune(id)) + "...",
					Name:    "Token " + string(rune(id)),
					Symbol:  "TK" + string(rune(id)),
				}
				cache.UpdateTokenConfigs([]*uregistrytypes.TokenConfig{newToken})

				// Occasional full updates
				if j%10 == 0 {
					cache.UpdateAll(chainConfigs, tokenConfigs)
				}
			}
		}(i)
	}

	// Wait for all goroutines to complete
	wg.Wait()

	// Verify cache is still in valid state
	chains := cache.GetAllChainConfigs()
	assert.NotNil(t, chains)
	assert.Greater(t, len(chains), 0)

	tokens := cache.GetAllTokenConfigs()
	assert.NotNil(t, tokens)
	assert.Greater(t, len(tokens), 0)
}

// TestConfigCacheEdgeCases tests edge cases and error conditions
func TestConfigCacheEdgeCases(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))
	cache := NewConfigCache(logger)

	t.Run("UpdateAll_NilSlices", func(t *testing.T) {
		// Should not panic with nil slices
		cache.UpdateAll(nil, nil)
		
		chains := cache.GetAllChainConfigs()
		assert.Len(t, chains, 0)
		
		tokens := cache.GetAllTokenConfigs()
		assert.Len(t, tokens, 0)
	})

	t.Run("UpdateAll_EmptySlices", func(t *testing.T) {
		// Should handle empty slices
		cache.UpdateAll([]*uregistrytypes.ChainConfig{}, []*uregistrytypes.TokenConfig{})
		
		chains := cache.GetAllChainConfigs()
		assert.Len(t, chains, 0)
		
		tokens := cache.GetAllTokenConfigs()
		assert.Len(t, tokens, 0)
	})

	t.Run("DuplicateChains", func(t *testing.T) {
		// Last one should win
		chainConfigs := []*uregistrytypes.ChainConfig{
			{
				Chain:          "eip155:11155111",
				VmType:         uregistrytypes.VmType_EVM,
				PublicRpcUrl:   "https://old.example.com",
				Enabled:        &uregistrytypes.ChainEnabled{IsInboundEnabled: false, IsOutboundEnabled: false},
			},
			{
				Chain:          "eip155:11155111",
				VmType:         uregistrytypes.VmType_EVM,
				PublicRpcUrl:   "https://new.example.com",
				Enabled:        &uregistrytypes.ChainEnabled{IsInboundEnabled: true, IsOutboundEnabled: true},
			},
		}

		cache.UpdateAll(chainConfigs, nil)
		
		config := cache.GetChainConfig("eip155:11155111")
		require.NotNil(t, config)
		assert.Equal(t, "https://new.example.com", config.PublicRpcUrl)
		assert.True(t, config.Enabled.IsInboundEnabled || config.Enabled.IsOutboundEnabled)
	})

	t.Run("DuplicateTokens", func(t *testing.T) {
		// Last one should win
		tokenConfigs := []*uregistrytypes.TokenConfig{
			{
				Chain:   "eip155:11155111",
				Address: "0xAAA...",
				Name:    "Old Token",
				Symbol:  "OLD",
			},
			{
				Chain:   "eip155:11155111",
				Address: "0xAAA...",
				Name:    "New Token",
				Symbol:  "NEW",
			},
		}

		cache.UpdateAll(nil, tokenConfigs)
		
		token := cache.GetTokenConfig("eip155:11155111", "0xAAA...")
		require.NotNil(t, token)
		assert.Equal(t, "New Token", token.Name)
		assert.Equal(t, "NEW", token.Symbol)
	})

	t.Run("TokensWithoutChainConfig", func(t *testing.T) {
		// Test adding tokens for a chain that doesn't have a chain config
		tokenConfigs := []*uregistrytypes.TokenConfig{
			{
				Chain:   "eip155:999",
				Address: "0xXXX...",
				Name:    "Orphan Token",
				Symbol:  "ORPH",
			},
		}

		cache.UpdateTokenConfigs(tokenConfigs)
		
		// Should still be able to get the token
		token := cache.GetTokenConfig("eip155:999", "0xXXX...")
		require.NotNil(t, token)
		assert.Equal(t, "Orphan Token", token.Name)

		// But chain config should be nil
		config := cache.GetChainConfig("eip155:999")
		assert.Nil(t, config)

		// GetChainData should return data with nil config
		chainData := cache.GetChainData("eip155:999")
		require.NotNil(t, chainData)
		assert.Nil(t, chainData.Config)
		assert.Len(t, chainData.Tokens, 1)
	})
}