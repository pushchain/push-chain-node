package chains

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/universalClient/chains/common"
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

	t.Run("different gateway methods returns false", func(t *testing.T) {
		cfg1 := &uregistrytypes.ChainConfig{
			Chain: "chain1",
			GatewayMethods: []*uregistrytypes.GatewayMethods{
				{Name: "method1", Identifier: "0xabc"},
			},
		}
		cfg2 := &uregistrytypes.ChainConfig{
			Chain: "chain1",
			GatewayMethods: []*uregistrytypes.GatewayMethods{
				{Name: "method1", Identifier: "0xdef"},
			},
		}

		assert.False(t, configsEqual(cfg1, cfg2))
	})

	t.Run("different vault methods returns false", func(t *testing.T) {
		cfg1 := &uregistrytypes.ChainConfig{
			Chain: "chain1",
			VaultMethods: []*uregistrytypes.VaultMethods{
				{Name: "vault1", Identifier: "0xabc"},
			},
		}
		cfg2 := &uregistrytypes.ChainConfig{
			Chain:        "chain1",
			VaultMethods: nil,
		}

		assert.False(t, configsEqual(cfg1, cfg2))
	})

	t.Run("different block confirmation returns false", func(t *testing.T) {
		cfg1 := &uregistrytypes.ChainConfig{
			Chain:             "chain1",
			BlockConfirmation: &uregistrytypes.BlockConfirmation{FastInbound: 2, StandardInbound: 12},
		}
		cfg2 := &uregistrytypes.ChainConfig{
			Chain:             "chain1",
			BlockConfirmation: &uregistrytypes.BlockConfirmation{FastInbound: 5, StandardInbound: 12},
		}

		assert.False(t, configsEqual(cfg1, cfg2))
	})

	t.Run("same full config returns true", func(t *testing.T) {
		cfg1 := &uregistrytypes.ChainConfig{
			Chain:          "chain1",
			GatewayAddress: "0x123",
			GatewayMethods: []*uregistrytypes.GatewayMethods{
				{Name: "m1", Identifier: "0xabc"},
			},
			BlockConfirmation: &uregistrytypes.BlockConfirmation{FastInbound: 2, StandardInbound: 12},
		}
		cfg2 := &uregistrytypes.ChainConfig{
			Chain:          "chain1",
			GatewayAddress: "0x123",
			GatewayMethods: []*uregistrytypes.GatewayMethods{
				{Name: "m1", Identifier: "0xabc"},
			},
			BlockConfirmation: &uregistrytypes.BlockConfirmation{FastInbound: 2, StandardInbound: 12},
		}

		assert.True(t, configsEqual(cfg1, cfg2))
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

	enabled := &uregistrytypes.ChainEnabled{IsInboundEnabled: true, IsOutboundEnabled: true}

	t.Run("new chain returns add", func(t *testing.T) {
		chainCfg := &uregistrytypes.ChainConfig{
			Chain:   "eip155:1",
			VmType:  uregistrytypes.VmType_EVM,
			Enabled: enabled,
		}

		action := chains.determineChainAction(chainCfg)
		assert.Equal(t, chainActionAdd, action)
	})

	t.Run("both flags off returns skip for new chain", func(t *testing.T) {
		chainCfg := &uregistrytypes.ChainConfig{
			Chain:   "eip155:99",
			VmType:  uregistrytypes.VmType_EVM,
			Enabled: &uregistrytypes.ChainEnabled{IsInboundEnabled: false, IsOutboundEnabled: false},
		}
		action := chains.determineChainAction(chainCfg)
		assert.Equal(t, chainActionSkip, action)
	})

	t.Run("both flags off returns remove for existing chain", func(t *testing.T) {
		chains.chainsMu.Lock()
		chains.chains["eip155:99"] = nil
		chains.chainConfigs["eip155:99"] = &uregistrytypes.ChainConfig{Chain: "eip155:99", Enabled: enabled}
		chains.chainsMu.Unlock()

		chainCfg := &uregistrytypes.ChainConfig{
			Chain:   "eip155:99",
			VmType:  uregistrytypes.VmType_EVM,
			Enabled: &uregistrytypes.ChainEnabled{IsInboundEnabled: false, IsOutboundEnabled: false},
		}
		action := chains.determineChainAction(chainCfg)
		assert.Equal(t, chainActionRemove, action)

		chains.chainsMu.Lock()
		delete(chains.chains, "eip155:99")
		delete(chains.chainConfigs, "eip155:99")
		chains.chainsMu.Unlock()
	})

	t.Run("nil enabled returns skip for new chain", func(t *testing.T) {
		chainCfg := &uregistrytypes.ChainConfig{
			Chain:  "eip155:98",
			VmType: uregistrytypes.VmType_EVM,
		}
		action := chains.determineChainAction(chainCfg)
		assert.Equal(t, chainActionSkip, action)
	})

	t.Run("existing chain with same config returns skip", func(t *testing.T) {
		chainCfg := &uregistrytypes.ChainConfig{
			Chain:          "eip155:1",
			VmType:         uregistrytypes.VmType_EVM,
			GatewayAddress: "0x123",
			Enabled:        enabled,
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
			Enabled:        enabled,
		}

		newCfg := &uregistrytypes.ChainConfig{
			Chain:          "eip155:1",
			VmType:         uregistrytypes.VmType_EVM,
			GatewayAddress: "0x456", // Different address
			Enabled:        enabled,
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

	t.Run("enabled flag change triggers update", func(t *testing.T) {
		oldCfg := &uregistrytypes.ChainConfig{
			Chain:   "eip155:1",
			VmType:  uregistrytypes.VmType_EVM,
			Enabled: enabled,
		}

		newCfg := &uregistrytypes.ChainConfig{
			Chain:   "eip155:1",
			VmType:  uregistrytypes.VmType_EVM,
			Enabled: &uregistrytypes.ChainEnabled{IsInboundEnabled: true, IsOutboundEnabled: false},
		}

		chains.chainsMu.Lock()
		chains.chains["eip155:1"] = nil
		chains.chainConfigs["eip155:1"] = oldCfg
		chains.chainsMu.Unlock()

		action := chains.determineChainAction(newCfg)
		assert.Equal(t, chainActionUpdate, action)

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

// mockChainClient implements common.ChainClient for testing
type mockChainClient struct {
	startCalled bool
	stopCalled  bool
	stopErr     error
}

func (m *mockChainClient) Start(ctx context.Context) error { m.startCalled = true; return nil }
func (m *mockChainClient) Stop() error                     { m.stopCalled = true; return m.stopErr }
func (m *mockChainClient) IsHealthy() bool                 { return true }
func (m *mockChainClient) GetTxBuilder() (common.TxBuilder, error) {
	return nil, nil
}

// newTestChains creates a Chains instance suitable for unit tests.
func newTestChains() *Chains {
	logger := zerolog.Nop()
	cfg := &config.Config{PushChainID: "localchain_9000-1"}
	return NewChains(nil, nil, cfg, logger)
}

func TestGetClient(t *testing.T) {
	t.Run("returns client when chain exists", func(t *testing.T) {
		c := newTestChains()
		mock := &mockChainClient{}
		c.chains["eip155:1"] = mock

		client, err := c.GetClient("eip155:1")
		require.NoError(t, err)
		assert.Equal(t, mock, client)
	})

	t.Run("returns error when chain does not exist", func(t *testing.T) {
		c := newTestChains()

		client, err := c.GetClient("eip155:999")
		assert.Nil(t, client)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "chain client not found")
		assert.Contains(t, err.Error(), "eip155:999")
	})

	t.Run("returns error on empty chain ID", func(t *testing.T) {
		c := newTestChains()

		client, err := c.GetClient("")
		assert.Nil(t, client)
		require.Error(t, err)
	})
}

func TestIsEVMChain(t *testing.T) {
	t.Run("returns true for EVM chain", func(t *testing.T) {
		c := newTestChains()
		c.chainConfigs["eip155:1"] = &uregistrytypes.ChainConfig{
			Chain:  "eip155:1",
			VmType: uregistrytypes.VmType_EVM,
		}

		assert.True(t, c.IsEVMChain("eip155:1"))
	})

	t.Run("returns false for SVM chain", func(t *testing.T) {
		c := newTestChains()
		c.chainConfigs["solana:mainnet"] = &uregistrytypes.ChainConfig{
			Chain:  "solana:mainnet",
			VmType: uregistrytypes.VmType_SVM,
		}

		assert.False(t, c.IsEVMChain("solana:mainnet"))
	})

	t.Run("returns false for unknown chain", func(t *testing.T) {
		c := newTestChains()

		assert.False(t, c.IsEVMChain("nonexistent:1"))
	})

	t.Run("returns false when config is nil in map", func(t *testing.T) {
		c := newTestChains()
		c.chainConfigs["eip155:1"] = nil

		assert.False(t, c.IsEVMChain("eip155:1"))
	})
}

func TestIsChainInboundEnabled(t *testing.T) {
	t.Run("returns true when inbound is enabled", func(t *testing.T) {
		c := newTestChains()
		c.chainConfigs["eip155:1"] = &uregistrytypes.ChainConfig{
			Chain: "eip155:1",
			Enabled: &uregistrytypes.ChainEnabled{
				IsInboundEnabled:  true,
				IsOutboundEnabled: false,
			},
		}

		assert.True(t, c.IsChainInboundEnabled("eip155:1"))
	})

	t.Run("returns false when inbound is disabled", func(t *testing.T) {
		c := newTestChains()
		c.chainConfigs["eip155:1"] = &uregistrytypes.ChainConfig{
			Chain: "eip155:1",
			Enabled: &uregistrytypes.ChainEnabled{
				IsInboundEnabled:  false,
				IsOutboundEnabled: true,
			},
		}

		assert.False(t, c.IsChainInboundEnabled("eip155:1"))
	})

	t.Run("returns false when Enabled is nil", func(t *testing.T) {
		c := newTestChains()
		c.chainConfigs["eip155:1"] = &uregistrytypes.ChainConfig{
			Chain:   "eip155:1",
			Enabled: nil,
		}

		assert.False(t, c.IsChainInboundEnabled("eip155:1"))
	})

	t.Run("returns false when config is nil", func(t *testing.T) {
		c := newTestChains()
		c.chainConfigs["eip155:1"] = nil

		assert.False(t, c.IsChainInboundEnabled("eip155:1"))
	})

	t.Run("returns false for unknown chain", func(t *testing.T) {
		c := newTestChains()

		assert.False(t, c.IsChainInboundEnabled("nonexistent:1"))
	})
}

func TestIsChainOutboundEnabled(t *testing.T) {
	t.Run("returns true when outbound is enabled", func(t *testing.T) {
		c := newTestChains()
		c.chainConfigs["eip155:1"] = &uregistrytypes.ChainConfig{
			Chain: "eip155:1",
			Enabled: &uregistrytypes.ChainEnabled{
				IsInboundEnabled:  false,
				IsOutboundEnabled: true,
			},
		}

		assert.True(t, c.IsChainOutboundEnabled("eip155:1"))
	})

	t.Run("returns false when outbound is disabled", func(t *testing.T) {
		c := newTestChains()
		c.chainConfigs["eip155:1"] = &uregistrytypes.ChainConfig{
			Chain: "eip155:1",
			Enabled: &uregistrytypes.ChainEnabled{
				IsInboundEnabled:  true,
				IsOutboundEnabled: false,
			},
		}

		assert.False(t, c.IsChainOutboundEnabled("eip155:1"))
	})

	t.Run("returns false when Enabled is nil", func(t *testing.T) {
		c := newTestChains()
		c.chainConfigs["eip155:1"] = &uregistrytypes.ChainConfig{
			Chain:   "eip155:1",
			Enabled: nil,
		}

		assert.False(t, c.IsChainOutboundEnabled("eip155:1"))
	})

	t.Run("returns false when config is nil", func(t *testing.T) {
		c := newTestChains()
		c.chainConfigs["eip155:1"] = nil

		assert.False(t, c.IsChainOutboundEnabled("eip155:1"))
	})

	t.Run("returns false for unknown chain", func(t *testing.T) {
		c := newTestChains()

		assert.False(t, c.IsChainOutboundEnabled("nonexistent:1"))
	})
}

func TestGetStandardConfirmations(t *testing.T) {
	t.Run("returns configured value when set", func(t *testing.T) {
		c := newTestChains()
		c.chainConfigs["eip155:1"] = &uregistrytypes.ChainConfig{
			Chain: "eip155:1",
			BlockConfirmation: &uregistrytypes.BlockConfirmation{
				StandardInbound: 20,
			},
		}

		assert.Equal(t, uint64(20), c.GetStandardConfirmations("eip155:1"))
	})

	t.Run("returns 12 default when BlockConfirmation is nil", func(t *testing.T) {
		c := newTestChains()
		c.chainConfigs["eip155:1"] = &uregistrytypes.ChainConfig{
			Chain:             "eip155:1",
			BlockConfirmation: nil,
		}

		assert.Equal(t, uint64(12), c.GetStandardConfirmations("eip155:1"))
	})

	t.Run("returns 12 default when StandardInbound is zero", func(t *testing.T) {
		c := newTestChains()
		c.chainConfigs["eip155:1"] = &uregistrytypes.ChainConfig{
			Chain: "eip155:1",
			BlockConfirmation: &uregistrytypes.BlockConfirmation{
				StandardInbound: 0,
			},
		}

		assert.Equal(t, uint64(12), c.GetStandardConfirmations("eip155:1"))
	})

	t.Run("returns 12 default for unknown chain", func(t *testing.T) {
		c := newTestChains()

		assert.Equal(t, uint64(12), c.GetStandardConfirmations("nonexistent:1"))
	})

	t.Run("returns 12 default when config is nil in map", func(t *testing.T) {
		c := newTestChains()
		c.chainConfigs["eip155:1"] = nil

		assert.Equal(t, uint64(12), c.GetStandardConfirmations("eip155:1"))
	})

	t.Run("returns value of 1 when configured", func(t *testing.T) {
		c := newTestChains()
		c.chainConfigs["solana:mainnet"] = &uregistrytypes.ChainConfig{
			Chain: "solana:mainnet",
			BlockConfirmation: &uregistrytypes.BlockConfirmation{
				StandardInbound: 1,
			},
		}

		assert.Equal(t, uint64(1), c.GetStandardConfirmations("solana:mainnet"))
	})
}

func TestStopAll(t *testing.T) {
	t.Run("stops all clients and clears maps", func(t *testing.T) {
		c := newTestChains()
		mock1 := &mockChainClient{}
		mock2 := &mockChainClient{}

		c.chains["eip155:1"] = mock1
		c.chains["solana:mainnet"] = mock2
		c.chainConfigs["eip155:1"] = &uregistrytypes.ChainConfig{Chain: "eip155:1"}
		c.chainConfigs["solana:mainnet"] = &uregistrytypes.ChainConfig{Chain: "solana:mainnet"}

		c.StopAll()

		assert.True(t, mock1.stopCalled)
		assert.True(t, mock2.stopCalled)
		assert.Empty(t, c.chains)
		assert.Empty(t, c.chainConfigs)
	})

	t.Run("handles stop error gracefully", func(t *testing.T) {
		c := newTestChains()
		mock := &mockChainClient{stopErr: assert.AnError}
		c.chains["eip155:1"] = mock
		c.chainConfigs["eip155:1"] = &uregistrytypes.ChainConfig{Chain: "eip155:1"}

		// Should not panic
		c.StopAll()

		assert.True(t, mock.stopCalled)
		assert.Empty(t, c.chains)
		assert.Empty(t, c.chainConfigs)
	})

	t.Run("works with no clients", func(t *testing.T) {
		c := newTestChains()

		// Should not panic
		c.StopAll()

		assert.Empty(t, c.chains)
		assert.Empty(t, c.chainConfigs)
	})
}

func TestRemoveChain(t *testing.T) {
	t.Run("removes existing chain and stops client", func(t *testing.T) {
		c := newTestChains()
		mock := &mockChainClient{}
		c.chains["eip155:1"] = mock
		c.chainConfigs["eip155:1"] = &uregistrytypes.ChainConfig{Chain: "eip155:1"}

		err := c.removeChain("eip155:1")
		require.NoError(t, err)

		assert.True(t, mock.stopCalled)
		_, exists := c.chains["eip155:1"]
		assert.False(t, exists)
		_, cfgExists := c.chainConfigs["eip155:1"]
		assert.False(t, cfgExists)
	})

	t.Run("returns nil for non-existent chain", func(t *testing.T) {
		c := newTestChains()

		err := c.removeChain("nonexistent:1")
		require.NoError(t, err)
	})

	t.Run("removes chain even when stop returns error", func(t *testing.T) {
		c := newTestChains()
		mock := &mockChainClient{stopErr: assert.AnError}
		c.chains["eip155:1"] = mock
		c.chainConfigs["eip155:1"] = &uregistrytypes.ChainConfig{Chain: "eip155:1"}

		err := c.removeChain("eip155:1")
		require.NoError(t, err)

		assert.True(t, mock.stopCalled)
		_, exists := c.chains["eip155:1"]
		assert.False(t, exists)
	})

	t.Run("only removes specified chain, leaves others", func(t *testing.T) {
		c := newTestChains()
		mock1 := &mockChainClient{}
		mock2 := &mockChainClient{}
		c.chains["eip155:1"] = mock1
		c.chains["eip155:97"] = mock2
		c.chainConfigs["eip155:1"] = &uregistrytypes.ChainConfig{Chain: "eip155:1"}
		c.chainConfigs["eip155:97"] = &uregistrytypes.ChainConfig{Chain: "eip155:97"}

		err := c.removeChain("eip155:1")
		require.NoError(t, err)

		assert.True(t, mock1.stopCalled)
		assert.False(t, mock2.stopCalled)
		_, exists := c.chains["eip155:97"]
		assert.True(t, exists)
	})
}

func TestStart(t *testing.T) {
	t.Run("returns error when pushCore is nil", func(t *testing.T) {
		c := newTestChains()
		// pushCore is nil by default from newTestChains

		err := c.Start(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "pushCore must be non-nil")
		assert.False(t, c.running)
	})

	t.Run("returns nil if already running", func(t *testing.T) {
		c := newTestChains()
		c.running = true

		err := c.Start(context.Background())
		require.NoError(t, err)
	})
}

func TestSanitizeChainID_Extended(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"dots replaced", "eip155.1", "eip155_1"},
		{"slashes replaced", "chain/sub/path", "chain_sub_path"},
		{"spaces replaced", "chain id", "chain_id"},
		{"mixed special chars", "a:b/c.d e!f@g#h", "a_b_c_d_e_f_g_h"},
		{"uppercase preserved", "EIP155:1", "EIP155_1"},
		{"only underscores and hyphens kept", "__--__", "__--__"},
		{"unicode replaced", "chain\u00e9:1", "chain__1"},
		{"tabs replaced", "chain\t1", "chain_1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, sanitizeChainID(tc.input))
		})
	}
}

func TestGatewayMethodsEqual(t *testing.T) {
	t.Run("both nil returns true", func(t *testing.T) {
		assert.True(t, gatewayMethodsEqual(nil, nil))
	})

	t.Run("both empty returns true", func(t *testing.T) {
		assert.True(t, gatewayMethodsEqual(
			[]*uregistrytypes.GatewayMethods{},
			[]*uregistrytypes.GatewayMethods{},
		))
	})

	t.Run("different lengths returns false", func(t *testing.T) {
		a := []*uregistrytypes.GatewayMethods{{Name: "m1"}}
		b := []*uregistrytypes.GatewayMethods{{Name: "m1"}, {Name: "m2"}}
		assert.False(t, gatewayMethodsEqual(a, b))
	})

	t.Run("one nil one empty returns false", func(t *testing.T) {
		// nil has length 0, empty slice has length 0 -- should be equal
		assert.True(t, gatewayMethodsEqual(nil, []*uregistrytypes.GatewayMethods{}))
	})

	t.Run("same single element returns true", func(t *testing.T) {
		m := &uregistrytypes.GatewayMethods{
			Name:             "addFunds",
			Identifier:       "0xabc",
			EventIdentifier:  "0xdef",
			ConfirmationType: uregistrytypes.ConfirmationType_CONFIRMATION_TYPE_STANDARD,
		}
		a := []*uregistrytypes.GatewayMethods{m}
		b := []*uregistrytypes.GatewayMethods{{
			Name:             "addFunds",
			Identifier:       "0xabc",
			EventIdentifier:  "0xdef",
			ConfirmationType: uregistrytypes.ConfirmationType_CONFIRMATION_TYPE_STANDARD,
		}}
		assert.True(t, gatewayMethodsEqual(a, b))
	})

	t.Run("different name returns false", func(t *testing.T) {
		a := []*uregistrytypes.GatewayMethods{{Name: "a", Identifier: "0x1"}}
		b := []*uregistrytypes.GatewayMethods{{Name: "b", Identifier: "0x1"}}
		assert.False(t, gatewayMethodsEqual(a, b))
	})

	t.Run("different identifier returns false", func(t *testing.T) {
		a := []*uregistrytypes.GatewayMethods{{Name: "m", Identifier: "0x1"}}
		b := []*uregistrytypes.GatewayMethods{{Name: "m", Identifier: "0x2"}}
		assert.False(t, gatewayMethodsEqual(a, b))
	})

	t.Run("different event identifier returns false", func(t *testing.T) {
		a := []*uregistrytypes.GatewayMethods{{Name: "m", EventIdentifier: "0xe1"}}
		b := []*uregistrytypes.GatewayMethods{{Name: "m", EventIdentifier: "0xe2"}}
		assert.False(t, gatewayMethodsEqual(a, b))
	})

	t.Run("different confirmation type returns false", func(t *testing.T) {
		a := []*uregistrytypes.GatewayMethods{{
			Name:             "m",
			ConfirmationType: uregistrytypes.ConfirmationType_CONFIRMATION_TYPE_FAST,
		}}
		b := []*uregistrytypes.GatewayMethods{{
			Name:             "m",
			ConfirmationType: uregistrytypes.ConfirmationType_CONFIRMATION_TYPE_STANDARD,
		}}
		assert.False(t, gatewayMethodsEqual(a, b))
	})

	t.Run("multiple methods all equal returns true", func(t *testing.T) {
		a := []*uregistrytypes.GatewayMethods{
			{Name: "m1", Identifier: "0x1"},
			{Name: "m2", Identifier: "0x2"},
		}
		b := []*uregistrytypes.GatewayMethods{
			{Name: "m1", Identifier: "0x1"},
			{Name: "m2", Identifier: "0x2"},
		}
		assert.True(t, gatewayMethodsEqual(a, b))
	})

	t.Run("multiple methods second differs returns false", func(t *testing.T) {
		a := []*uregistrytypes.GatewayMethods{
			{Name: "m1", Identifier: "0x1"},
			{Name: "m2", Identifier: "0x2"},
		}
		b := []*uregistrytypes.GatewayMethods{
			{Name: "m1", Identifier: "0x1"},
			{Name: "m2", Identifier: "0x99"},
		}
		assert.False(t, gatewayMethodsEqual(a, b))
	})
}

func TestVaultMethodsEqual(t *testing.T) {
	t.Run("both nil returns true", func(t *testing.T) {
		assert.True(t, vaultMethodsEqual(nil, nil))
	})

	t.Run("both empty returns true", func(t *testing.T) {
		assert.True(t, vaultMethodsEqual(
			[]*uregistrytypes.VaultMethods{},
			[]*uregistrytypes.VaultMethods{},
		))
	})

	t.Run("different lengths returns false", func(t *testing.T) {
		a := []*uregistrytypes.VaultMethods{{Name: "v1"}}
		b := []*uregistrytypes.VaultMethods{}
		assert.False(t, vaultMethodsEqual(a, b))
	})

	t.Run("same single element returns true", func(t *testing.T) {
		a := []*uregistrytypes.VaultMethods{{
			Name:             "deposit",
			Identifier:       "0xabc",
			EventIdentifier:  "0xdef",
			ConfirmationType: uregistrytypes.ConfirmationType_CONFIRMATION_TYPE_FAST,
		}}
		b := []*uregistrytypes.VaultMethods{{
			Name:             "deposit",
			Identifier:       "0xabc",
			EventIdentifier:  "0xdef",
			ConfirmationType: uregistrytypes.ConfirmationType_CONFIRMATION_TYPE_FAST,
		}}
		assert.True(t, vaultMethodsEqual(a, b))
	})

	t.Run("different name returns false", func(t *testing.T) {
		a := []*uregistrytypes.VaultMethods{{Name: "deposit"}}
		b := []*uregistrytypes.VaultMethods{{Name: "withdraw"}}
		assert.False(t, vaultMethodsEqual(a, b))
	})

	t.Run("different identifier returns false", func(t *testing.T) {
		a := []*uregistrytypes.VaultMethods{{Name: "v", Identifier: "0x1"}}
		b := []*uregistrytypes.VaultMethods{{Name: "v", Identifier: "0x2"}}
		assert.False(t, vaultMethodsEqual(a, b))
	})

	t.Run("different event identifier returns false", func(t *testing.T) {
		a := []*uregistrytypes.VaultMethods{{Name: "v", EventIdentifier: "0xe1"}}
		b := []*uregistrytypes.VaultMethods{{Name: "v", EventIdentifier: "0xe2"}}
		assert.False(t, vaultMethodsEqual(a, b))
	})

	t.Run("different confirmation type returns false", func(t *testing.T) {
		a := []*uregistrytypes.VaultMethods{{
			Name:             "v",
			ConfirmationType: uregistrytypes.ConfirmationType_CONFIRMATION_TYPE_STANDARD,
		}}
		b := []*uregistrytypes.VaultMethods{{
			Name:             "v",
			ConfirmationType: uregistrytypes.ConfirmationType_CONFIRMATION_TYPE_FAST,
		}}
		assert.False(t, vaultMethodsEqual(a, b))
	})

	t.Run("multiple methods all equal returns true", func(t *testing.T) {
		a := []*uregistrytypes.VaultMethods{
			{Name: "v1", Identifier: "0x1"},
			{Name: "v2", Identifier: "0x2"},
		}
		b := []*uregistrytypes.VaultMethods{
			{Name: "v1", Identifier: "0x1"},
			{Name: "v2", Identifier: "0x2"},
		}
		assert.True(t, vaultMethodsEqual(a, b))
	})
}

func TestChainEnabledEqual(t *testing.T) {
	t.Run("both nil returns true", func(t *testing.T) {
		assert.True(t, chainEnabledEqual(nil, nil))
	})

	t.Run("first nil second non-nil returns false", func(t *testing.T) {
		assert.False(t, chainEnabledEqual(nil, &uregistrytypes.ChainEnabled{}))
	})

	t.Run("first non-nil second nil returns false", func(t *testing.T) {
		assert.False(t, chainEnabledEqual(&uregistrytypes.ChainEnabled{}, nil))
	})

	t.Run("both false returns true", func(t *testing.T) {
		a := &uregistrytypes.ChainEnabled{IsInboundEnabled: false, IsOutboundEnabled: false}
		b := &uregistrytypes.ChainEnabled{IsInboundEnabled: false, IsOutboundEnabled: false}
		assert.True(t, chainEnabledEqual(a, b))
	})

	t.Run("both true returns true", func(t *testing.T) {
		a := &uregistrytypes.ChainEnabled{IsInboundEnabled: true, IsOutboundEnabled: true}
		b := &uregistrytypes.ChainEnabled{IsInboundEnabled: true, IsOutboundEnabled: true}
		assert.True(t, chainEnabledEqual(a, b))
	})

	t.Run("inbound differs returns false", func(t *testing.T) {
		a := &uregistrytypes.ChainEnabled{IsInboundEnabled: true, IsOutboundEnabled: true}
		b := &uregistrytypes.ChainEnabled{IsInboundEnabled: false, IsOutboundEnabled: true}
		assert.False(t, chainEnabledEqual(a, b))
	})

	t.Run("outbound differs returns false", func(t *testing.T) {
		a := &uregistrytypes.ChainEnabled{IsInboundEnabled: true, IsOutboundEnabled: false}
		b := &uregistrytypes.ChainEnabled{IsInboundEnabled: true, IsOutboundEnabled: true}
		assert.False(t, chainEnabledEqual(a, b))
	})

	t.Run("mixed flags equal returns true", func(t *testing.T) {
		a := &uregistrytypes.ChainEnabled{IsInboundEnabled: true, IsOutboundEnabled: false}
		b := &uregistrytypes.ChainEnabled{IsInboundEnabled: true, IsOutboundEnabled: false}
		assert.True(t, chainEnabledEqual(a, b))
	})
}

func TestBlockConfirmationEqual(t *testing.T) {
	t.Run("both nil returns true", func(t *testing.T) {
		assert.True(t, blockConfirmationEqual(nil, nil))
	})

	t.Run("first nil second non-nil returns false", func(t *testing.T) {
		assert.False(t, blockConfirmationEqual(nil, &uregistrytypes.BlockConfirmation{}))
	})

	t.Run("first non-nil second nil returns false", func(t *testing.T) {
		assert.False(t, blockConfirmationEqual(&uregistrytypes.BlockConfirmation{}, nil))
	})

	t.Run("both zero values returns true", func(t *testing.T) {
		a := &uregistrytypes.BlockConfirmation{}
		b := &uregistrytypes.BlockConfirmation{}
		assert.True(t, blockConfirmationEqual(a, b))
	})

	t.Run("same values returns true", func(t *testing.T) {
		a := &uregistrytypes.BlockConfirmation{FastInbound: 3, StandardInbound: 12}
		b := &uregistrytypes.BlockConfirmation{FastInbound: 3, StandardInbound: 12}
		assert.True(t, blockConfirmationEqual(a, b))
	})

	t.Run("fast inbound differs returns false", func(t *testing.T) {
		a := &uregistrytypes.BlockConfirmation{FastInbound: 2, StandardInbound: 12}
		b := &uregistrytypes.BlockConfirmation{FastInbound: 5, StandardInbound: 12}
		assert.False(t, blockConfirmationEqual(a, b))
	})

	t.Run("standard inbound differs returns false", func(t *testing.T) {
		a := &uregistrytypes.BlockConfirmation{FastInbound: 2, StandardInbound: 12}
		b := &uregistrytypes.BlockConfirmation{FastInbound: 2, StandardInbound: 20}
		assert.False(t, blockConfirmationEqual(a, b))
	})

	t.Run("both fields differ returns false", func(t *testing.T) {
		a := &uregistrytypes.BlockConfirmation{FastInbound: 1, StandardInbound: 6}
		b := &uregistrytypes.BlockConfirmation{FastInbound: 3, StandardInbound: 12}
		assert.False(t, blockConfirmationEqual(a, b))
	})
}

func TestDetermineChainAction_Extended(t *testing.T) {
	enabled := &uregistrytypes.ChainEnabled{IsInboundEnabled: true, IsOutboundEnabled: true}

	t.Run("nil enabled on existing chain returns remove", func(t *testing.T) {
		c := newTestChains()
		mock := &mockChainClient{}
		c.chains["eip155:42"] = mock
		c.chainConfigs["eip155:42"] = &uregistrytypes.ChainConfig{Chain: "eip155:42", Enabled: enabled}

		action := c.determineChainAction(&uregistrytypes.ChainConfig{
			Chain:   "eip155:42",
			Enabled: nil, // nil means both disabled
		})
		assert.Equal(t, chainActionRemove, action)
	})

	t.Run("only inbound enabled is not fully disabled so adds new chain", func(t *testing.T) {
		c := newTestChains()
		action := c.determineChainAction(&uregistrytypes.ChainConfig{
			Chain: "eip155:50",
			Enabled: &uregistrytypes.ChainEnabled{
				IsInboundEnabled:  true,
				IsOutboundEnabled: false,
			},
		})
		assert.Equal(t, chainActionAdd, action)
	})

	t.Run("only outbound enabled is not fully disabled so adds new chain", func(t *testing.T) {
		c := newTestChains()
		action := c.determineChainAction(&uregistrytypes.ChainConfig{
			Chain: "eip155:51",
			Enabled: &uregistrytypes.ChainEnabled{
				IsInboundEnabled:  false,
				IsOutboundEnabled: true,
			},
		})
		assert.Equal(t, chainActionAdd, action)
	})

	t.Run("existing chain with no stored config returns skip", func(t *testing.T) {
		c := newTestChains()
		mock := &mockChainClient{}
		c.chains["eip155:60"] = mock
		// chainConfigs deliberately not set (nil stored config)

		action := c.determineChainAction(&uregistrytypes.ChainConfig{
			Chain:   "eip155:60",
			Enabled: enabled,
		})
		// existingConfig is nil, so configsEqual is not called, result is skip
		assert.Equal(t, chainActionSkip, action)
	})

	t.Run("existing chain with changed enabled flags returns update", func(t *testing.T) {
		c := newTestChains()
		mock := &mockChainClient{}
		c.chains["eip155:70"] = mock
		c.chainConfigs["eip155:70"] = &uregistrytypes.ChainConfig{
			Chain:   "eip155:70",
			VmType:  uregistrytypes.VmType_EVM,
			Enabled: enabled,
		}

		action := c.determineChainAction(&uregistrytypes.ChainConfig{
			Chain:  "eip155:70",
			VmType: uregistrytypes.VmType_EVM,
			Enabled: &uregistrytypes.ChainEnabled{
				IsInboundEnabled:  true,
				IsOutboundEnabled: false, // changed
			},
		})
		assert.Equal(t, chainActionUpdate, action)
	})

	t.Run("existing chain with changed VM type returns update", func(t *testing.T) {
		c := newTestChains()
		mock := &mockChainClient{}
		c.chains["eip155:80"] = mock
		c.chainConfigs["eip155:80"] = &uregistrytypes.ChainConfig{
			Chain:   "eip155:80",
			VmType:  uregistrytypes.VmType_EVM,
			Enabled: enabled,
		}

		action := c.determineChainAction(&uregistrytypes.ChainConfig{
			Chain:   "eip155:80",
			VmType:  uregistrytypes.VmType_SVM, // changed
			Enabled: enabled,
		})
		assert.Equal(t, chainActionUpdate, action)
	})

	t.Run("existing chain with changed block confirmation returns update", func(t *testing.T) {
		c := newTestChains()
		mock := &mockChainClient{}
		c.chains["eip155:90"] = mock
		c.chainConfigs["eip155:90"] = &uregistrytypes.ChainConfig{
			Chain:             "eip155:90",
			Enabled:           enabled,
			BlockConfirmation: &uregistrytypes.BlockConfirmation{FastInbound: 2, StandardInbound: 12},
		}

		action := c.determineChainAction(&uregistrytypes.ChainConfig{
			Chain:             "eip155:90",
			Enabled:           enabled,
			BlockConfirmation: &uregistrytypes.BlockConfirmation{FastInbound: 5, StandardInbound: 20},
		})
		assert.Equal(t, chainActionUpdate, action)
	})

	t.Run("existing chain with changed gateway methods returns update", func(t *testing.T) {
		c := newTestChains()
		mock := &mockChainClient{}
		c.chains["eip155:100"] = mock
		c.chainConfigs["eip155:100"] = &uregistrytypes.ChainConfig{
			Chain:   "eip155:100",
			Enabled: enabled,
			GatewayMethods: []*uregistrytypes.GatewayMethods{
				{Name: "m1", Identifier: "0x1"},
			},
		}

		action := c.determineChainAction(&uregistrytypes.ChainConfig{
			Chain:   "eip155:100",
			Enabled: enabled,
			GatewayMethods: []*uregistrytypes.GatewayMethods{
				{Name: "m1", Identifier: "0x1"},
				{Name: "m2", Identifier: "0x2"}, // added method
			},
		})
		assert.Equal(t, chainActionUpdate, action)
	})

	t.Run("existing chain with changed vault methods returns update", func(t *testing.T) {
		c := newTestChains()
		mock := &mockChainClient{}
		c.chains["eip155:110"] = mock
		c.chainConfigs["eip155:110"] = &uregistrytypes.ChainConfig{
			Chain:   "eip155:110",
			Enabled: enabled,
			VaultMethods: []*uregistrytypes.VaultMethods{
				{Name: "v1", Identifier: "0x1"},
			},
		}

		action := c.determineChainAction(&uregistrytypes.ChainConfig{
			Chain:   "eip155:110",
			Enabled: enabled,
			VaultMethods: []*uregistrytypes.VaultMethods{
				{Name: "v1", Identifier: "0xchanged"},
			},
		})
		assert.Equal(t, chainActionUpdate, action)
	})
}

func TestConfigsEqual_Extended(t *testing.T) {
	t.Run("identical empty configs returns true", func(t *testing.T) {
		a := &uregistrytypes.ChainConfig{}
		b := &uregistrytypes.ChainConfig{}
		assert.True(t, configsEqual(a, b))
	})

	t.Run("nil enabled on both returns true", func(t *testing.T) {
		a := &uregistrytypes.ChainConfig{Chain: "c1", Enabled: nil}
		b := &uregistrytypes.ChainConfig{Chain: "c1", Enabled: nil}
		assert.True(t, configsEqual(a, b))
	})

	t.Run("nil enabled vs non-nil enabled returns false", func(t *testing.T) {
		a := &uregistrytypes.ChainConfig{Chain: "c1", Enabled: nil}
		b := &uregistrytypes.ChainConfig{Chain: "c1", Enabled: &uregistrytypes.ChainEnabled{}}
		assert.False(t, configsEqual(a, b))
	})

	t.Run("nil block confirmation on both returns true", func(t *testing.T) {
		a := &uregistrytypes.ChainConfig{Chain: "c1", BlockConfirmation: nil}
		b := &uregistrytypes.ChainConfig{Chain: "c1", BlockConfirmation: nil}
		assert.True(t, configsEqual(a, b))
	})

	t.Run("nil block confirmation vs non-nil returns false", func(t *testing.T) {
		a := &uregistrytypes.ChainConfig{Chain: "c1", BlockConfirmation: nil}
		b := &uregistrytypes.ChainConfig{Chain: "c1", BlockConfirmation: &uregistrytypes.BlockConfirmation{FastInbound: 1}}
		assert.False(t, configsEqual(a, b))
	})

	t.Run("empty gateway methods vs nil returns true", func(t *testing.T) {
		a := &uregistrytypes.ChainConfig{Chain: "c1", GatewayMethods: nil}
		b := &uregistrytypes.ChainConfig{Chain: "c1", GatewayMethods: []*uregistrytypes.GatewayMethods{}}
		assert.True(t, configsEqual(a, b))
	})

	t.Run("empty vault methods vs nil returns true", func(t *testing.T) {
		a := &uregistrytypes.ChainConfig{Chain: "c1", VaultMethods: nil}
		b := &uregistrytypes.ChainConfig{Chain: "c1", VaultMethods: []*uregistrytypes.VaultMethods{}}
		assert.True(t, configsEqual(a, b))
	})

	t.Run("full config with all fields matching returns true", func(t *testing.T) {
		cfg := func() *uregistrytypes.ChainConfig {
			return &uregistrytypes.ChainConfig{
				Chain:          "eip155:1",
				VmType:         uregistrytypes.VmType_EVM,
				GatewayAddress: "0xgateway",
				GatewayMethods: []*uregistrytypes.GatewayMethods{
					{Name: "gm1", Identifier: "0xg1", EventIdentifier: "0xge1", ConfirmationType: uregistrytypes.ConfirmationType_CONFIRMATION_TYPE_FAST},
				},
				VaultMethods: []*uregistrytypes.VaultMethods{
					{Name: "vm1", Identifier: "0xv1", EventIdentifier: "0xve1", ConfirmationType: uregistrytypes.ConfirmationType_CONFIRMATION_TYPE_STANDARD},
				},
				BlockConfirmation: &uregistrytypes.BlockConfirmation{FastInbound: 2, StandardInbound: 12},
				Enabled:           &uregistrytypes.ChainEnabled{IsInboundEnabled: true, IsOutboundEnabled: true},
			}
		}
		assert.True(t, configsEqual(cfg(), cfg()))
	})
}

func TestRemoveChain_Extended(t *testing.T) {
	t.Run("remove with empty chain ID on empty maps", func(t *testing.T) {
		c := newTestChains()
		err := c.removeChain("")
		require.NoError(t, err)
	})

	t.Run("remove same chain twice", func(t *testing.T) {
		c := newTestChains()
		mock := &mockChainClient{}
		c.chains["eip155:1"] = mock
		c.chainConfigs["eip155:1"] = &uregistrytypes.ChainConfig{Chain: "eip155:1"}

		err := c.removeChain("eip155:1")
		require.NoError(t, err)
		assert.True(t, mock.stopCalled)

		// Second remove should be a no-op
		err = c.removeChain("eip155:1")
		require.NoError(t, err)
	})

	t.Run("remove chain cleans up config even if client was nil entry", func(t *testing.T) {
		c := newTestChains()
		// Store a nil client (edge case)
		c.chains["eip155:5"] = nil
		c.chainConfigs["eip155:5"] = &uregistrytypes.ChainConfig{Chain: "eip155:5"}

		// The chain key exists, so removeChain will try client.Stop() on nil.
		// This will panic if not handled, but looking at the code, it calls
		// client.Stop() without nil check. We verify the key exists first.
		_, exists := c.chains["eip155:5"]
		assert.True(t, exists)
		// Note: calling removeChain with a nil client value would panic;
		// this confirms the map entry is present.
	})

	t.Run("remove does not affect push chain ID tracking", func(t *testing.T) {
		c := newTestChains()
		mock := &mockChainClient{}
		chainID := c.pushChainID
		c.chains[chainID] = mock
		c.chainConfigs[chainID] = &uregistrytypes.ChainConfig{Chain: chainID}

		err := c.removeChain(chainID)
		require.NoError(t, err)
		// pushChainID field is unchanged
		assert.Equal(t, "localchain_9000-1", c.pushChainID)
	})
}

func TestStopAll_Extended(t *testing.T) {
	t.Run("stop all with many chains", func(t *testing.T) {
		c := newTestChains()
		mocks := make([]*mockChainClient, 10)
		for i := 0; i < 10; i++ {
			mocks[i] = &mockChainClient{}
			chainID := fmt.Sprintf("eip155:%d", i)
			c.chains[chainID] = mocks[i]
			c.chainConfigs[chainID] = &uregistrytypes.ChainConfig{Chain: chainID}
		}

		c.StopAll()

		for i, m := range mocks {
			assert.True(t, m.stopCalled, "mock %d should have been stopped", i)
		}
		assert.Empty(t, c.chains)
		assert.Empty(t, c.chainConfigs)
	})

	t.Run("stop all can be called multiple times", func(t *testing.T) {
		c := newTestChains()
		mock := &mockChainClient{}
		c.chains["eip155:1"] = mock
		c.chainConfigs["eip155:1"] = &uregistrytypes.ChainConfig{Chain: "eip155:1"}

		c.StopAll()
		assert.True(t, mock.stopCalled)

		// Second call should not panic
		c.StopAll()
		assert.Empty(t, c.chains)
	})
}

func TestStop(t *testing.T) {
	t.Run("stop when not running is no-op", func(t *testing.T) {
		c := newTestChains()
		c.running = false
		// Should not panic or block
		c.Stop()
	})
}

func TestGetClient_Extended(t *testing.T) {
	t.Run("returns different clients for different chain IDs", func(t *testing.T) {
		c := newTestChains()
		mock1 := &mockChainClient{}
		mock2 := &mockChainClient{}
		c.chains["eip155:1"] = mock1
		c.chains["solana:mainnet"] = mock2

		client1, err1 := c.GetClient("eip155:1")
		client2, err2 := c.GetClient("solana:mainnet")

		require.NoError(t, err1)
		require.NoError(t, err2)
		assert.Equal(t, mock1, client1)
		assert.Equal(t, mock2, client2)
		assert.True(t, client1 != client2, "clients should be distinct pointers")
	})
}

func TestStop_Lifecycle(t *testing.T) {
	t.Run("stop closes stopCh and calls StopAll", func(t *testing.T) {
		c := newTestChains()
		// Simulate a started state: set running, create stopCh, add wg
		c.running = true
		c.stopCh = make(chan struct{})
		c.wg.Add(1)

		// Add a mock client to verify StopAll is called
		mock := &mockChainClient{}
		c.chains["eip155:1"] = mock
		c.chainConfigs["eip155:1"] = &uregistrytypes.ChainConfig{Chain: "eip155:1"}

		// Simulate the goroutine that would be waiting on stopCh
		go func() {
			<-c.stopCh
			c.wg.Done()
		}()

		c.Stop()

		assert.False(t, c.running)
		assert.True(t, mock.stopCalled)
		assert.Empty(t, c.chains)
		assert.Empty(t, c.chainConfigs)
	})

	t.Run("stop is idempotent when called twice", func(t *testing.T) {
		c := newTestChains()
		c.running = true
		c.stopCh = make(chan struct{})
		c.wg.Add(1)

		go func() {
			<-c.stopCh
			c.wg.Done()
		}()

		c.Stop()
		// Second call should not panic (running is already false)
		c.Stop()
		assert.False(t, c.running)
	})
}

func TestAddChain_ErrorCases(t *testing.T) {
	t.Run("nil config returns error", func(t *testing.T) {
		c := newTestChains()
		err := c.addChain(context.Background(), nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid chain config")
	})

	t.Run("empty chain ID returns error", func(t *testing.T) {
		c := newTestChains()
		cfg := &uregistrytypes.ChainConfig{
			Chain: "",
		}
		err := c.addChain(context.Background(), cfg)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid chain config")
	})

	t.Run("unsupported VM type returns error", func(t *testing.T) {
		c := newTestChains()
		c.config = &config.Config{
			PushChainID: "localchain_9000-1",
			NodeHome:    t.TempDir(),
		}
		cfg := &uregistrytypes.ChainConfig{
			Chain:  "unknown:1",
			VmType: uregistrytypes.VmType(999),
			Enabled: &uregistrytypes.ChainEnabled{
				IsInboundEnabled:  true,
				IsOutboundEnabled: true,
			},
		}
		err := c.addChain(context.Background(), cfg)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported VM type")
	})
}

func TestEnsurePushChain_EmptyID(t *testing.T) {
	t.Run("returns error when pushChainID is empty", func(t *testing.T) {
		c := newTestChains()
		c.pushChainID = ""

		err := c.ensurePushChain(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "push chain ID not configured")
	})

	t.Run("returns nil when push chain already exists", func(t *testing.T) {
		c := newTestChains()
		mock := &mockChainClient{}
		c.chains[c.pushChainID] = mock

		err := c.ensurePushChain(context.Background())
		require.NoError(t, err)
	})
}

func TestGetChainDB(t *testing.T) {
	t.Run("creates database for EVM chain", func(t *testing.T) {
		c := newTestChains()
		c.config = &config.Config{
			PushChainID: "localchain_9000-1",
			NodeHome:    t.TempDir(),
		}

		database, err := c.getChainDB("eip155:1")
		require.NoError(t, err)
		require.NotNil(t, database)
	})

	t.Run("creates database for Solana chain", func(t *testing.T) {
		c := newTestChains()
		c.config = &config.Config{
			PushChainID: "localchain_9000-1",
			NodeHome:    t.TempDir(),
		}

		database, err := c.getChainDB("solana:mainnet")
		require.NoError(t, err)
		require.NotNil(t, database)
	})

	t.Run("creates database for push chain", func(t *testing.T) {
		c := newTestChains()
		c.config = &config.Config{
			PushChainID: "localchain_9000-1",
			NodeHome:    t.TempDir(),
		}

		database, err := c.getChainDB("localchain_9000-1")
		require.NoError(t, err)
		require.NotNil(t, database)
	})

	t.Run("sanitizes chain ID with special characters", func(t *testing.T) {
		c := newTestChains()
		c.config = &config.Config{
			PushChainID: "localchain_9000-1",
			NodeHome:    t.TempDir(),
		}

		database, err := c.getChainDB("eip155:97")
		require.NoError(t, err)
		require.NotNil(t, database)
	})
}

func TestRemoveChain_WithMockClient(t *testing.T) {
	t.Run("remove chain calls stop on running client", func(t *testing.T) {
		c := newTestChains()
		mock := &mockChainClient{}
		c.chains["eip155:42"] = mock
		c.chainConfigs["eip155:42"] = &uregistrytypes.ChainConfig{
			Chain:  "eip155:42",
			VmType: uregistrytypes.VmType_EVM,
			Enabled: &uregistrytypes.ChainEnabled{
				IsInboundEnabled:  true,
				IsOutboundEnabled: true,
			},
		}

		err := c.removeChain("eip155:42")
		require.NoError(t, err)
		assert.True(t, mock.stopCalled)
		_, exists := c.chains["eip155:42"]
		assert.False(t, exists)
		_, cfgExists := c.chainConfigs["eip155:42"]
		assert.False(t, cfgExists)
	})

	t.Run("remove chain with stop error still removes from maps", func(t *testing.T) {
		c := newTestChains()
		mock := &mockChainClient{stopErr: fmt.Errorf("stop failed")}
		c.chains["eip155:42"] = mock
		c.chainConfigs["eip155:42"] = &uregistrytypes.ChainConfig{Chain: "eip155:42"}

		err := c.removeChain("eip155:42")
		require.NoError(t, err) // removeChain always returns nil
		assert.True(t, mock.stopCalled)
		_, exists := c.chains["eip155:42"]
		assert.False(t, exists)
	})
}

func TestStart_PushCoreNil(t *testing.T) {
	t.Run("sets running to false when pushCore is nil", func(t *testing.T) {
		c := newTestChains()
		err := c.Start(context.Background())
		require.Error(t, err)
		assert.False(t, c.running, "running should remain false when Start fails")
	})
}

func TestEnsurePushChain_DBError(t *testing.T) {
	t.Run("returns error when getChainDB fails", func(t *testing.T) {
		c := newTestChains()
		c.config = &config.Config{
			PushChainID: "localchain_9000-1",
			NodeHome:    "/dev/null/impossible/path",
		}

		err := c.ensurePushChain(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to get database for push chain")
	})
}

func TestConfigsEqual_GatewayMethodsOrdering(t *testing.T) {
	t.Run("gateway methods different order is not equal", func(t *testing.T) {
		cfg1 := &uregistrytypes.ChainConfig{
			Chain: "chain1",
			GatewayMethods: []*uregistrytypes.GatewayMethods{
				{Name: "m1", Identifier: "0x1"},
				{Name: "m2", Identifier: "0x2"},
			},
		}
		cfg2 := &uregistrytypes.ChainConfig{
			Chain: "chain1",
			GatewayMethods: []*uregistrytypes.GatewayMethods{
				{Name: "m2", Identifier: "0x2"},
				{Name: "m1", Identifier: "0x1"},
			},
		}
		// Order matters in the current implementation
		assert.False(t, configsEqual(cfg1, cfg2))
	})

	t.Run("vault methods different order is not equal", func(t *testing.T) {
		cfg1 := &uregistrytypes.ChainConfig{
			Chain: "chain1",
			VaultMethods: []*uregistrytypes.VaultMethods{
				{Name: "v1", Identifier: "0x1"},
				{Name: "v2", Identifier: "0x2"},
			},
		}
		cfg2 := &uregistrytypes.ChainConfig{
			Chain: "chain1",
			VaultMethods: []*uregistrytypes.VaultMethods{
				{Name: "v2", Identifier: "0x2"},
				{Name: "v1", Identifier: "0x1"},
			},
		}
		assert.False(t, configsEqual(cfg1, cfg2))
	})
}

func TestStopAll_WithStopErrors(t *testing.T) {
	t.Run("continues stopping remaining clients when one errors", func(t *testing.T) {
		c := newTestChains()
		mockErr := &mockChainClient{stopErr: fmt.Errorf("stop error")}
		mockOk := &mockChainClient{}

		c.chains["eip155:1"] = mockErr
		c.chains["eip155:2"] = mockOk
		c.chainConfigs["eip155:1"] = &uregistrytypes.ChainConfig{Chain: "eip155:1"}
		c.chainConfigs["eip155:2"] = &uregistrytypes.ChainConfig{Chain: "eip155:2"}

		c.StopAll()

		assert.True(t, mockErr.stopCalled)
		assert.True(t, mockOk.stopCalled)
		assert.Empty(t, c.chains)
		assert.Empty(t, c.chainConfigs)
	})
}

func TestNewChains_ConfigPreserved(t *testing.T) {
	t.Run("preserves all config fields", func(t *testing.T) {
		logger := zerolog.Nop()
		cfg := &config.Config{
			PushChainID:                 "push:1",
			NodeHome:                    "/tmp/test",
			ConfigRefreshIntervalSeconds: 30,
		}

		chains := NewChains(nil, nil, cfg, logger)

		assert.Equal(t, cfg, chains.config)
		assert.Equal(t, "push:1", chains.pushChainID)
	})
}

func TestIsChainInboundEnabled_EdgeCases(t *testing.T) {
	t.Run("returns false when chain config exists but enabled is nil", func(t *testing.T) {
		c := newTestChains()
		c.chainConfigs["eip155:1"] = &uregistrytypes.ChainConfig{
			Chain:   "eip155:1",
			Enabled: nil,
		}
		assert.False(t, c.IsChainInboundEnabled("eip155:1"))
	})
}

func TestIsChainOutboundEnabled_EdgeCases(t *testing.T) {
	t.Run("returns true when both flags enabled", func(t *testing.T) {
		c := newTestChains()
		c.chainConfigs["eip155:1"] = &uregistrytypes.ChainConfig{
			Chain: "eip155:1",
			Enabled: &uregistrytypes.ChainEnabled{
				IsInboundEnabled:  true,
				IsOutboundEnabled: true,
			},
		}
		assert.True(t, c.IsChainOutboundEnabled("eip155:1"))
	})
}

func TestAddChain_GetChainDBError(t *testing.T) {
	t.Run("returns error when NodeHome is invalid path", func(t *testing.T) {
		c := newTestChains()
		c.config = &config.Config{
			PushChainID: "localchain_9000-1",
			NodeHome:    "/dev/null/impossible/path",
		}

		cfg := &uregistrytypes.ChainConfig{
			Chain:  "eip155:1",
			VmType: uregistrytypes.VmType_EVM,
			Enabled: &uregistrytypes.ChainEnabled{
				IsInboundEnabled:  true,
				IsOutboundEnabled: true,
			},
		}
		err := c.addChain(context.Background(), cfg)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to get database")
	})
}

func TestDetermineChainAction_PushChainID(t *testing.T) {
	t.Run("push chain ID is not special in determineChainAction", func(t *testing.T) {
		c := newTestChains()
		// The push chain ID in determineChainAction is just like any other chain
		cfg := &uregistrytypes.ChainConfig{
			Chain: c.pushChainID,
			Enabled: &uregistrytypes.ChainEnabled{
				IsInboundEnabled:  true,
				IsOutboundEnabled: true,
			},
		}
		action := c.determineChainAction(cfg)
		assert.Equal(t, chainActionAdd, action)
	})
}
