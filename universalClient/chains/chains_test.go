package chains

import (
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/universalClient/config"
	uregistrytypes "github.com/pushchain/push-chain-node/x/uregistry/types"
)

func TestNewChains(t *testing.T) {
	t.Run("creates chains manager with valid config", func(t *testing.T) {
		logger := zerolog.Nop()
		cfg := &config.Config{
			PushChainID: "localchain_9000-1",
		}

		chains := NewChains(nil, nil, cfg, logger)

		require.NotNil(t, chains)
		assert.NotNil(t, chains.chains)
		assert.NotNil(t, chains.chainConfigs)
		assert.Equal(t, "localchain_9000-1", chains.pushChainID)
	})

	t.Run("initializes empty maps", func(t *testing.T) {
		logger := zerolog.Nop()
		cfg := &config.Config{
			PushChainID: "test-chain",
		}

		chains := NewChains(nil, nil, cfg, logger)

		assert.Empty(t, chains.chains)
		assert.Empty(t, chains.chainConfigs)
	})
}

func TestSanitizeChainID(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "EVM chain ID with colon",
			input:    "eip155:1",
			expected: "eip155_1",
		},
		{
			name:     "EVM chain ID BSC",
			input:    "eip155:97",
			expected: "eip155_97",
		},
		{
			name:     "Solana mainnet",
			input:    "solana:mainnet",
			expected: "solana_mainnet",
		},
		{
			name:     "Already sanitized",
			input:    "eip155_1",
			expected: "eip155_1",
		},
		{
			name:     "With hyphen",
			input:    "localchain_9000-1",
			expected: "localchain_9000-1",
		},
		{
			name:     "Multiple special chars",
			input:    "chain:id:with:colons",
			expected: "chain_id_with_colons",
		},
		{
			name:     "Empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "Alphanumeric only",
			input:    "ethereum1",
			expected: "ethereum1",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := sanitizeChainID(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestConfigsEqual(t *testing.T) {
	t.Run("both nil returns true", func(t *testing.T) {
		assert.True(t, configsEqual(nil, nil))
	})

	t.Run("one nil returns false", func(t *testing.T) {
		cfg := &uregistrytypes.ChainConfig{Chain: "eip155:1"}
		assert.False(t, configsEqual(cfg, nil))
		assert.False(t, configsEqual(nil, cfg))
	})

	t.Run("equal configs returns true", func(t *testing.T) {
		cfg1 := &uregistrytypes.ChainConfig{
			Chain:          "eip155:1",
			VmType:         uregistrytypes.VmType_EVM,
			GatewayAddress: "0x123",
			Enabled: &uregistrytypes.ChainEnabled{
				IsInboundEnabled:  true,
				IsOutboundEnabled: true,
			},
		}
		cfg2 := &uregistrytypes.ChainConfig{
			Chain:          "eip155:1",
			VmType:         uregistrytypes.VmType_EVM,
			GatewayAddress: "0x123",
			Enabled: &uregistrytypes.ChainEnabled{
				IsInboundEnabled:  true,
				IsOutboundEnabled: true,
			},
		}

		assert.True(t, configsEqual(cfg1, cfg2))
	})

	t.Run("different chain ID returns false", func(t *testing.T) {
		cfg1 := &uregistrytypes.ChainConfig{Chain: "eip155:1"}
		cfg2 := &uregistrytypes.ChainConfig{Chain: "eip155:97"}

		assert.False(t, configsEqual(cfg1, cfg2))
	})

	t.Run("different VM type returns false", func(t *testing.T) {
		cfg1 := &uregistrytypes.ChainConfig{
			Chain:  "chain1",
			VmType: uregistrytypes.VmType_EVM,
		}
		cfg2 := &uregistrytypes.ChainConfig{
			Chain:  "chain1",
			VmType: uregistrytypes.VmType_SVM,
		}

		assert.False(t, configsEqual(cfg1, cfg2))
	})

	t.Run("different gateway address returns false", func(t *testing.T) {
		cfg1 := &uregistrytypes.ChainConfig{
			Chain:          "chain1",
			GatewayAddress: "0x123",
		}
		cfg2 := &uregistrytypes.ChainConfig{
			Chain:          "chain1",
			GatewayAddress: "0x456",
		}

		assert.False(t, configsEqual(cfg1, cfg2))
	})

	t.Run("different enabled state returns false", func(t *testing.T) {
		cfg1 := &uregistrytypes.ChainConfig{
			Chain: "chain1",
			Enabled: &uregistrytypes.ChainEnabled{
				IsInboundEnabled:  true,
				IsOutboundEnabled: true,
			},
		}
		cfg2 := &uregistrytypes.ChainConfig{
			Chain: "chain1",
			Enabled: &uregistrytypes.ChainEnabled{
				IsInboundEnabled:  false,
				IsOutboundEnabled: true,
			},
		}

		assert.False(t, configsEqual(cfg1, cfg2))
	})

	t.Run("both enabled nil returns true", func(t *testing.T) {
		cfg1 := &uregistrytypes.ChainConfig{
			Chain:   "chain1",
			Enabled: nil,
		}
		cfg2 := &uregistrytypes.ChainConfig{
			Chain:   "chain1",
			Enabled: nil,
		}

		assert.True(t, configsEqual(cfg1, cfg2))
	})

	t.Run("one enabled nil returns false", func(t *testing.T) {
		cfg1 := &uregistrytypes.ChainConfig{
			Chain: "chain1",
			Enabled: &uregistrytypes.ChainEnabled{
				IsInboundEnabled: true,
			},
		}
		cfg2 := &uregistrytypes.ChainConfig{
			Chain:   "chain1",
			Enabled: nil,
		}

		assert.False(t, configsEqual(cfg1, cfg2))
	})
}

func TestChainAction(t *testing.T) {
	t.Run("chain action constants", func(t *testing.T) {
		assert.Equal(t, chainAction(0), chainActionSkip)
		assert.Equal(t, chainAction(1), chainActionAdd)
		assert.Equal(t, chainAction(2), chainActionUpdate)
		assert.Equal(t, chainAction(3), chainActionRemove)
	})
}

func TestChainsStruct(t *testing.T) {
	t.Run("struct has expected fields", func(t *testing.T) {
		c := &Chains{}
		assert.Nil(t, c.pushCore)
		assert.Nil(t, c.pushSigner)
		assert.Nil(t, c.config)
		assert.Nil(t, c.chains)
		assert.Nil(t, c.chainConfigs)
		assert.Empty(t, c.pushChainID)
		assert.False(t, c.running)
	})
}

func TestDetermineChainAction(t *testing.T) {
	logger := zerolog.Nop()
	cfg := &config.Config{
		PushChainID: "localchain_9000-1",
	}
	chains := NewChains(nil, nil, cfg, logger)

	t.Run("disabled chain returns skip", func(t *testing.T) {
		chainCfg := &uregistrytypes.ChainConfig{
			Chain:   "eip155:1",
			Enabled: nil,
		}

		action := chains.determineChainAction(chainCfg)
		assert.Equal(t, chainActionSkip, action)
	})

	t.Run("disabled inbound and outbound returns skip", func(t *testing.T) {
		chainCfg := &uregistrytypes.ChainConfig{
			Chain: "eip155:1",
			Enabled: &uregistrytypes.ChainEnabled{
				IsInboundEnabled:  false,
				IsOutboundEnabled: false,
			},
		}

		action := chains.determineChainAction(chainCfg)
		assert.Equal(t, chainActionSkip, action)
	})

	t.Run("new enabled chain returns add", func(t *testing.T) {
		chainCfg := &uregistrytypes.ChainConfig{
			Chain: "eip155:1",
			Enabled: &uregistrytypes.ChainEnabled{
				IsInboundEnabled:  true,
				IsOutboundEnabled: false,
			},
		}

		action := chains.determineChainAction(chainCfg)
		assert.Equal(t, chainActionAdd, action)
	})

	t.Run("existing chain with same config returns skip", func(t *testing.T) {
		chainCfg := &uregistrytypes.ChainConfig{
			Chain:          "eip155:1",
			VmType:         uregistrytypes.VmType_EVM,
			GatewayAddress: "0x123",
			Enabled: &uregistrytypes.ChainEnabled{
				IsInboundEnabled:  true,
				IsOutboundEnabled: true,
			},
		}

		// Add the chain first
		chains.chainsMu.Lock()
		chains.chains["eip155:1"] = nil // Mock client
		chains.chainConfigs["eip155:1"] = chainCfg
		chains.chainsMu.Unlock()

		action := chains.determineChainAction(chainCfg)
		assert.Equal(t, chainActionSkip, action)

		// Cleanup
		chains.chainsMu.Lock()
		delete(chains.chains, "eip155:1")
		delete(chains.chainConfigs, "eip155:1")
		chains.chainsMu.Unlock()
	})

	t.Run("existing chain with different config returns update", func(t *testing.T) {
		oldCfg := &uregistrytypes.ChainConfig{
			Chain:          "eip155:1",
			VmType:         uregistrytypes.VmType_EVM,
			GatewayAddress: "0x123",
			Enabled: &uregistrytypes.ChainEnabled{
				IsInboundEnabled:  true,
				IsOutboundEnabled: true,
			},
		}

		newCfg := &uregistrytypes.ChainConfig{
			Chain:          "eip155:1",
			VmType:         uregistrytypes.VmType_EVM,
			GatewayAddress: "0x456", // Different address
			Enabled: &uregistrytypes.ChainEnabled{
				IsInboundEnabled:  true,
				IsOutboundEnabled: true,
			},
		}

		// Add the chain first
		chains.chainsMu.Lock()
		chains.chains["eip155:1"] = nil // Mock client
		chains.chainConfigs["eip155:1"] = oldCfg
		chains.chainsMu.Unlock()

		action := chains.determineChainAction(newCfg)
		assert.Equal(t, chainActionUpdate, action)

		// Cleanup
		chains.chainsMu.Lock()
		delete(chains.chains, "eip155:1")
		delete(chains.chainConfigs, "eip155:1")
		chains.chainsMu.Unlock()
	})
}

func TestPerSyncTimeout(t *testing.T) {
	t.Run("per sync timeout is 30 seconds", func(t *testing.T) {
		assert.Equal(t, 30*time.Second, perSyncTimeout)
	})
}
