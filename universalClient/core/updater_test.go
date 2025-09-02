package core

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/universalClient/chains"
	"github.com/pushchain/push-chain-node/universalClient/config"
	"github.com/pushchain/push-chain-node/universalClient/registry"
	uregistrytypes "github.com/pushchain/push-chain-node/x/uregistry/types"
)

// MockedRegistryClient implements RegistryInterface for testing
type MockedRegistryClient struct {
	getAllChainConfigsFunc func(ctx context.Context) ([]*uregistrytypes.ChainConfig, error)
	getAllTokenConfigsFunc func(ctx context.Context) ([]*uregistrytypes.TokenConfig, error)
}

// getTestConfig returns a test configuration
func getTestConfig(updatePeriod time.Duration) *config.Config {
	return &config.Config{
		ConfigRefreshInterval: updatePeriod,
		InitialFetchRetries:   3,
		InitialFetchTimeout:   5 * time.Second,
		RetryBackoff:          100 * time.Millisecond,
	}
}

func (m *MockedRegistryClient) GetAllChainConfigs(ctx context.Context) ([]*uregistrytypes.ChainConfig, error) {
	if m.getAllChainConfigsFunc != nil {
		return m.getAllChainConfigsFunc(ctx)
	}
	return nil, nil
}

func (m *MockedRegistryClient) GetAllTokenConfigs(ctx context.Context) ([]*uregistrytypes.TokenConfig, error) {
	if m.getAllTokenConfigsFunc != nil {
		return m.getAllTokenConfigsFunc(ctx)
	}
	return nil, nil
}

// TestConfigUpdaterInitialization tests the creation of ConfigUpdater
func TestConfigUpdaterInitialization(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))

	// Create real instances for initialization test
	cache := registry.NewConfigCache(logger)
	chainReg := chains.NewChainRegistry(logger)
	cfg := &config.Config{
		ConfigRefreshInterval: 5 * time.Minute,
		InitialFetchRetries:   5,
		InitialFetchTimeout:   30 * time.Second,
		RetryBackoff:          time.Second,
	}

	updater := NewConfigUpdater(
		nil, // Registry client can be nil for initialization test
		cache,
		chainReg,
		cfg,
		logger,
	)

	assert.NotNil(t, updater)
	assert.Equal(t, 5*time.Minute, updater.updatePeriod)
	assert.Equal(t, cfg, updater.config)
	assert.NotNil(t, updater.stopCh)
	assert.NotNil(t, updater.logger)
}

// TestConfigUpdaterUpdateConfigs tests the updateConfigs method
func TestConfigUpdaterUpdateConfigs(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))

	t.Run("Successful update", func(t *testing.T) {
		cache := registry.NewConfigCache(logger)
		chainReg := chains.NewChainRegistry(logger)

		chainConfigs := []*uregistrytypes.ChainConfig{
			{
				Chain:          "eip155:11155111",
				VmType:         uregistrytypes.VmType_EVM,
				PublicRpcUrl:   "https://eth-sepolia.example.com",
				GatewayAddress: "0x123...",
				Enabled: &uregistrytypes.ChainEnabled{
					IsInboundEnabled:  true,
					IsOutboundEnabled: true,
				},
			},
			{
				Chain:          "solana:EtWTRABZaYq6iMfeYKouRu166VU2xqa1",
				VmType:         uregistrytypes.VmType_SVM,
				PublicRpcUrl:   "https://api.devnet.solana.com",
				GatewayAddress: "Sol123...",
				Enabled: &uregistrytypes.ChainEnabled{
					IsInboundEnabled:  true,
					IsOutboundEnabled: true,
				},
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

		mockRegistry := &MockedRegistryClient{
			getAllChainConfigsFunc: func(ctx context.Context) ([]*uregistrytypes.ChainConfig, error) {
				return chainConfigs, nil
			},
			getAllTokenConfigsFunc: func(ctx context.Context) ([]*uregistrytypes.TokenConfig, error) {
				return tokenConfigs, nil
			},
		}

		updater := &ConfigUpdater{
			registry: mockRegistry,
			cache:    cache,
			chainReg: chainReg,
			config:   getTestConfig(10 * time.Minute),
			logger:   logger,
		}

		ctx := context.Background()
		err := updater.updateConfigs(ctx)
		require.NoError(t, err)

		// Verify cache was updated
		cachedChains := cache.GetAllChainConfigs()
		assert.Len(t, cachedChains, 2)

		cachedTokens := cache.GetAllTokenConfigs()
		assert.Len(t, cachedTokens, 1)
	})

	t.Run("Chain config fetch error", func(t *testing.T) {
		cache := registry.NewConfigCache(logger)
		chainReg := chains.NewChainRegistry(logger)

		mockRegistry := &MockedRegistryClient{
			getAllChainConfigsFunc: func(ctx context.Context) ([]*uregistrytypes.ChainConfig, error) {
				return nil, errors.New("chain fetch error")
			},
		}

		updater := &ConfigUpdater{
			registry: mockRegistry,
			cache:    cache,
			chainReg: chainReg,
			config:   getTestConfig(10 * time.Minute),
			logger:   logger,
		}

		ctx := context.Background()
		err := updater.updateConfigs(ctx)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to fetch chain configs")
	})

	t.Run("Token config fetch error", func(t *testing.T) {
		cache := registry.NewConfigCache(logger)
		chainReg := chains.NewChainRegistry(logger)

		mockRegistry := &MockedRegistryClient{
			getAllChainConfigsFunc: func(ctx context.Context) ([]*uregistrytypes.ChainConfig, error) {
				return []*uregistrytypes.ChainConfig{}, nil
			},
			getAllTokenConfigsFunc: func(ctx context.Context) ([]*uregistrytypes.TokenConfig, error) {
				return nil, errors.New("token fetch error")
			},
		}

		updater := &ConfigUpdater{
			registry: mockRegistry,
			cache:    cache,
			chainReg: chainReg,
			config:   getTestConfig(10 * time.Minute),
			logger:   logger,
		}

		ctx := context.Background()
		err := updater.updateConfigs(ctx)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to fetch token configs")
	})

	t.Run("Context timeout", func(t *testing.T) {
		cache := registry.NewConfigCache(logger)
		chainReg := chains.NewChainRegistry(logger)

		mockRegistry := &MockedRegistryClient{
			getAllChainConfigsFunc: func(ctx context.Context) ([]*uregistrytypes.ChainConfig, error) {
				// Simulate slow response
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(100 * time.Millisecond):
					return []*uregistrytypes.ChainConfig{}, nil
				}
			},
		}

		updater := &ConfigUpdater{
			registry: mockRegistry,
			cache:    cache,
			chainReg: chainReg,
			config:   getTestConfig(10 * time.Minute),
			logger:   logger,
		}

		// Use updateConfigs method directly with a short timeout
		// Create our own timeout since updateConfigs creates its own
		err := updater.updateConfigs(context.Background())
		// This should succeed because updateConfigs uses 30s timeout
		assert.NoError(t, err)
	})
}

// TestConfigUpdaterUpdateChainClients tests chain client updates
func TestConfigUpdaterUpdateChainClients(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))
	cache := registry.NewConfigCache(logger)
	chainReg := chains.NewChainRegistry(logger)

	updater := &ConfigUpdater{
		cache:    cache,
		chainReg: chainReg,
		config:   getTestConfig(10 * time.Minute),
		logger:   logger,
	}

	t.Run("Add enabled chains", func(t *testing.T) {
		chainConfigs := []*uregistrytypes.ChainConfig{
			{
				Chain:        "eip155:11155111",
				VmType:       uregistrytypes.VmType_EVM,
				PublicRpcUrl: "https://eth-sepolia.example.com",
				Enabled: &uregistrytypes.ChainEnabled{
					IsInboundEnabled:  true,
					IsOutboundEnabled: true,
				},
			},
			{
				Chain:        "solana:EtWTRABZaYq6iMfeYKouRu166VU2xqa1",
				VmType:       uregistrytypes.VmType_SVM,
				PublicRpcUrl: "https://api.devnet.solana.com",
				Enabled: &uregistrytypes.ChainEnabled{
					IsInboundEnabled:  true,
					IsOutboundEnabled: true,
				},
			},
		}

		ctx := context.Background()
		err := updater.updateChainClients(ctx, chainConfigs)
		// This will not error because updateChainClients continues on individual chain errors
		assert.NoError(t, err)

		// Solana chain will be added (devnet is public), but EVM will fail (invalid URL)
		allChains := chainReg.GetAllChains()
		assert.Len(t, allChains, 1) // Only Solana succeeds
		assert.Contains(t, allChains, "solana:EtWTRABZaYq6iMfeYKouRu166VU2xqa1")
	})

	t.Run("Skip disabled chains", func(t *testing.T) {
		chainConfigs := []*uregistrytypes.ChainConfig{
			{
				Chain:  "eip155:11155111",
				VmType: uregistrytypes.VmType_EVM,
				Enabled: &uregistrytypes.ChainEnabled{
					IsInboundEnabled:  false,
					IsOutboundEnabled: false,
				}, // Disabled TODO: Check this once @shoaib
			},
		}

		ctx := context.Background()
		err := updater.updateChainClients(ctx, chainConfigs)
		assert.NoError(t, err)

		// No chains should be added
		allChains := chainReg.GetAllChains()
		assert.Len(t, allChains, 0)
	})

	t.Run("Skip nil configs", func(t *testing.T) {
		chainConfigs := []*uregistrytypes.ChainConfig{
			nil, // Nil config
			{
				Chain:  "", // Empty chain ID
				VmType: uregistrytypes.VmType_EVM,
				Enabled: &uregistrytypes.ChainEnabled{
					IsInboundEnabled:  true,
					IsOutboundEnabled: true,
				},
			},
		}

		ctx := context.Background()
		err := updater.updateChainClients(ctx, chainConfigs)
		assert.NoError(t, err)

		// No chains should be added
		allChains := chainReg.GetAllChains()
		assert.Len(t, allChains, 0)
	})
}

// TestConfigUpdaterForceUpdate tests the ForceUpdate method
func TestConfigUpdaterForceUpdate(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))
	cache := registry.NewConfigCache(logger)
	chainReg := chains.NewChainRegistry(logger)

	forceUpdateCalled := false
	mockRegistry := &MockedRegistryClient{
		getAllChainConfigsFunc: func(ctx context.Context) ([]*uregistrytypes.ChainConfig, error) {
			forceUpdateCalled = true
			return []*uregistrytypes.ChainConfig{
				{
					Chain:  "eip155:11155111",
					VmType: uregistrytypes.VmType_EVM,
					Enabled: &uregistrytypes.ChainEnabled{
						IsInboundEnabled:  true,
						IsOutboundEnabled: true,
					},
				},
			}, nil
		},
		getAllTokenConfigsFunc: func(ctx context.Context) ([]*uregistrytypes.TokenConfig, error) {
			return []*uregistrytypes.TokenConfig{}, nil
		},
	}

	updater := &ConfigUpdater{
		registry: mockRegistry,
		cache:    cache,
		chainReg: chainReg,
		logger:   logger,
	}

	ctx := context.Background()
	err := updater.ForceUpdate(ctx)
	require.NoError(t, err)

	assert.True(t, forceUpdateCalled)

	// Verify cache was updated
	chains := cache.GetAllChainConfigs()
	assert.Len(t, chains, 1)
}

// TestConfigUpdaterPeriodicUpdates tests periodic update functionality
func TestConfigUpdaterPeriodicUpdates(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))
	cache := registry.NewConfigCache(logger)
	chainReg := chains.NewChainRegistry(logger)

	var updateCount int32
	mockRegistry := &MockedRegistryClient{
		getAllChainConfigsFunc: func(ctx context.Context) ([]*uregistrytypes.ChainConfig, error) {
			atomic.AddInt32(&updateCount, 1)
			return []*uregistrytypes.ChainConfig{
				{
					Chain:  "eip155:11155111",
					VmType: uregistrytypes.VmType_EVM,
					Enabled: &uregistrytypes.ChainEnabled{
						IsInboundEnabled:  true,
						IsOutboundEnabled: true,
					},
				},
			}, nil
		},
		getAllTokenConfigsFunc: func(ctx context.Context) ([]*uregistrytypes.TokenConfig, error) {
			return []*uregistrytypes.TokenConfig{}, nil
		},
	}

	testCfg := getTestConfig(50 * time.Millisecond) // Short period for testing
	updater := &ConfigUpdater{
		registry:     mockRegistry,
		cache:        cache,
		chainReg:     chainReg,
		config:       testCfg,
		updatePeriod: testCfg.ConfigRefreshInterval,
		logger:       logger,
		stopCh:       make(chan struct{}),
	}

	ctx := context.Background()
	err := updater.Start(ctx)
	require.NoError(t, err)

	// Wait for at least 2 periodic updates
	time.Sleep(150 * time.Millisecond)

	// Should have initial update + at least 2 periodic updates
	count := atomic.LoadInt32(&updateCount)
	assert.GreaterOrEqual(t, count, int32(3))

	updater.Stop()
}

// TestConfigUpdaterStartStop tests start and stop functionality
func TestConfigUpdaterStartStop(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))
	cache := registry.NewConfigCache(logger)
	chainReg := chains.NewChainRegistry(logger)

	mockRegistry := &MockedRegistryClient{
		getAllChainConfigsFunc: func(ctx context.Context) ([]*uregistrytypes.ChainConfig, error) {
			return []*uregistrytypes.ChainConfig{}, nil
		},
		getAllTokenConfigsFunc: func(ctx context.Context) ([]*uregistrytypes.TokenConfig, error) {
			return []*uregistrytypes.TokenConfig{}, nil
		},
	}

	testCfg := getTestConfig(50 * time.Millisecond)
	updater := &ConfigUpdater{
		registry:     mockRegistry,
		cache:        cache,
		chainReg:     chainReg,
		config:       testCfg,
		updatePeriod: testCfg.ConfigRefreshInterval,
		logger:       logger,
		stopCh:       make(chan struct{}),
	}

	ctx := context.Background()

	// Start the updater
	err := updater.Start(ctx)
	require.NoError(t, err)

	// Verify ticker was created
	assert.NotNil(t, updater.ticker)

	// Stop the updater
	updater.Stop()

	// Give it a moment to stop
	time.Sleep(10 * time.Millisecond)

	// Verify stop channel is closed by trying to send to it
	select {
	case <-updater.stopCh:
		// Channel is closed, which is expected
	default:
		t.Fatal("stop channel should be closed")
	}
}

// TestConfigUpdaterContextCancellation tests context cancellation handling
func TestConfigUpdaterContextCancellation(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))
	cache := registry.NewConfigCache(logger)
	chainReg := chains.NewChainRegistry(logger)

	mockRegistry := &MockedRegistryClient{
		getAllChainConfigsFunc: func(ctx context.Context) ([]*uregistrytypes.ChainConfig, error) {
			return []*uregistrytypes.ChainConfig{}, nil
		},
		getAllTokenConfigsFunc: func(ctx context.Context) ([]*uregistrytypes.TokenConfig, error) {
			return []*uregistrytypes.TokenConfig{}, nil
		},
	}

	testCfg := getTestConfig(50 * time.Millisecond)
	updater := &ConfigUpdater{
		registry:     mockRegistry,
		cache:        cache,
		chainReg:     chainReg,
		config:       testCfg,
		updatePeriod: testCfg.ConfigRefreshInterval,
		logger:       logger,
		stopCh:       make(chan struct{}),
	}

	// Create a cancellable context
	ctx, cancel := context.WithCancel(context.Background())

	// Start the updater
	err := updater.Start(ctx)
	require.NoError(t, err)

	// Let it run briefly
	time.Sleep(20 * time.Millisecond)

	// Cancel the context
	cancel()

	// Wait a bit to ensure the goroutine has time to stop
	time.Sleep(100 * time.Millisecond)

	// The updater should handle context cancellation gracefully
	// We can't directly test if the goroutine stopped, but no panic should occur
}

// TestConfigUpdaterStartFailure tests handling of initial update failure
func TestConfigUpdaterStartFailure(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))
	cache := registry.NewConfigCache(logger)
	chainReg := chains.NewChainRegistry(logger)

	mockRegistry := &MockedRegistryClient{
		getAllChainConfigsFunc: func(ctx context.Context) ([]*uregistrytypes.ChainConfig, error) {
			return nil, errors.New("initial update error")
		},
	}

	testCfg := getTestConfig(50 * time.Millisecond)
	updater := &ConfigUpdater{
		registry:     mockRegistry,
		cache:        cache,
		chainReg:     chainReg,
		config:       testCfg,
		updatePeriod: testCfg.ConfigRefreshInterval,
		logger:       logger,
		stopCh:       make(chan struct{}),
	}

	ctx := context.Background()

	// Start should fail if all initial update attempts fail
	err := updater.Start(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to perform initial configuration update")
}

// TestConfigUpdaterInitialUpdateRetries tests the retry logic for initial configuration fetch
func TestConfigUpdaterInitialUpdateRetries(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))
	cache := registry.NewConfigCache(logger)
	chainReg := chains.NewChainRegistry(logger)

	attemptCount := 0
	mockRegistry := &MockedRegistryClient{
		getAllChainConfigsFunc: func(ctx context.Context) ([]*uregistrytypes.ChainConfig, error) {
			attemptCount++
			if attemptCount < 3 {
				// Fail first 2 attempts
				return nil, errors.New("temporary error")
			}
			// Succeed on 3rd attempt
			return []*uregistrytypes.ChainConfig{
				{
					Chain:  "eip155:11155111",
					VmType: uregistrytypes.VmType_EVM,
					Enabled: &uregistrytypes.ChainEnabled{
						IsInboundEnabled:  true,
						IsOutboundEnabled: true,
					},
				},
			}, nil
		},
		getAllTokenConfigsFunc: func(ctx context.Context) ([]*uregistrytypes.TokenConfig, error) {
			return []*uregistrytypes.TokenConfig{}, nil
		},
	}

	testCfg := getTestConfig(50 * time.Millisecond)
	updater := &ConfigUpdater{
		registry:     mockRegistry,
		cache:        cache,
		chainReg:     chainReg,
		config:       testCfg,
		updatePeriod: testCfg.ConfigRefreshInterval,
		logger:       logger,
		stopCh:       make(chan struct{}),
	}

	ctx := context.Background()

	// Start should succeed after retries
	err := updater.Start(ctx)
	assert.NoError(t, err)
	assert.Equal(t, 3, attemptCount) // Should have made 3 attempts

	// Verify cache was updated
	chains := cache.GetAllChainConfigs()
	assert.Len(t, chains, 1)

	// Stop the updater
	updater.Stop()
}

// TestConfigUpdaterInitialUpdateTimeout tests timeout handling during initial fetch
func TestConfigUpdaterInitialUpdateTimeout(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))
	cache := registry.NewConfigCache(logger)
	chainReg := chains.NewChainRegistry(logger)

	mockRegistry := &MockedRegistryClient{
		getAllChainConfigsFunc: func(ctx context.Context) ([]*uregistrytypes.ChainConfig, error) {
			// Simulate slow response
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(10 * time.Second):
				return []*uregistrytypes.ChainConfig{}, nil
			}
		},
	}

	testCfg := getTestConfig(50 * time.Millisecond)
	testCfg.InitialFetchTimeout = 100 * time.Millisecond // Very short timeout
	testCfg.InitialFetchRetries = 1                      // Single attempt

	updater := &ConfigUpdater{
		registry:     mockRegistry,
		cache:        cache,
		chainReg:     chainReg,
		config:       testCfg,
		updatePeriod: testCfg.ConfigRefreshInterval,
		logger:       logger,
		stopCh:       make(chan struct{}),
	}

	ctx := context.Background()

	// Start should fail due to timeout
	err := updater.Start(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to perform initial configuration update")
}
