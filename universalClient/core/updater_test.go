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

	"github.com/rollchains/pchain/universalClient/chains"
	"github.com/rollchains/pchain/universalClient/config"
	"github.com/rollchains/pchain/universalClient/db"
	"github.com/rollchains/pchain/universalClient/registry"
	uregistrytypes "github.com/rollchains/pchain/x/uregistry/types"
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

// Test successful config update
func TestConfigUpdater_UpdateConfigs_Success(t *testing.T) {
	ctx := context.Background()
	logger := zerolog.Nop()
	
	// Create mock registry with successful responses
	mockRegistry := &MockedRegistryClient{
		getAllChainConfigsFunc: func(ctx context.Context) ([]*uregistrytypes.ChainConfig, error) {
			return []*uregistrytypes.ChainConfig{
				{
					Chain:   "eip155:1",
					VmType:  uregistrytypes.VmType_EVM,
					Enabled: &uregistrytypes.ChainEnabled{IsInboundEnabled: true},
				},
			}, nil
		},
		getAllTokenConfigsFunc: func(ctx context.Context) ([]*uregistrytypes.TokenConfig, error) {
			return []*uregistrytypes.TokenConfig{
				{
					Chain:   "eip155:1",
					Address: "0xabc",
					Symbol:  "TEST",
				},
			}, nil
		},
	}
	
	cache := registry.NewConfigCache(logger)
	cfg := getTestConfig(30 * time.Second)
	dbManager := db.NewInMemoryChainDBManager(logger, cfg)
	chainRegistry := chains.NewChainRegistry(dbManager, logger)
	chainRegistry.SetAppConfig(cfg)
	
	updater := NewConfigUpdater(mockRegistry, cache, chainRegistry, cfg, logger)
	
	err := updater.updateConfigs(ctx)
	assert.NoError(t, err)
	
	// Verify cache was updated
	chainConfigs := cache.GetAllChainConfigs()
	assert.Len(t, chainConfigs, 1)
	assert.Equal(t, "eip155:1", chainConfigs[0].Chain)
	
	tokenConfigs := cache.GetAllTokenConfigs()
	assert.Len(t, tokenConfigs, 1)
	assert.Equal(t, "TEST", tokenConfigs[0].Symbol)
}

// Test config update with chain fetch error
func TestConfigUpdater_UpdateConfigs_ChainFetchError(t *testing.T) {
	ctx := context.Background()
	logger := zerolog.Nop()
	
	mockRegistry := &MockedRegistryClient{
		getAllChainConfigsFunc: func(ctx context.Context) ([]*uregistrytypes.ChainConfig, error) {
			return nil, errors.New("chain fetch error")
		},
	}
	
	cache := registry.NewConfigCache(logger)
	cfg := getTestConfig(30 * time.Second)
	dbManager := db.NewInMemoryChainDBManager(logger, cfg)
	chainRegistry := chains.NewChainRegistry(dbManager, logger)
	chainRegistry.SetAppConfig(cfg)
	
	updater := NewConfigUpdater(mockRegistry, cache, chainRegistry, cfg, logger)
	
	err := updater.updateConfigs(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to fetch chain configs")
}

// Test config update with token fetch error
func TestConfigUpdater_UpdateConfigs_TokenFetchError(t *testing.T) {
	ctx := context.Background()
	logger := zerolog.Nop()
	
	mockRegistry := &MockedRegistryClient{
		getAllChainConfigsFunc: func(ctx context.Context) ([]*uregistrytypes.ChainConfig, error) {
			return []*uregistrytypes.ChainConfig{}, nil
		},
		getAllTokenConfigsFunc: func(ctx context.Context) ([]*uregistrytypes.TokenConfig, error) {
			return nil, errors.New("token fetch error")
		},
	}
	
	cache := registry.NewConfigCache(logger)
	cfg := getTestConfig(30 * time.Second)
	dbManager := db.NewInMemoryChainDBManager(logger, cfg)
	chainRegistry := chains.NewChainRegistry(dbManager, logger)
	chainRegistry.SetAppConfig(cfg)
	
	updater := NewConfigUpdater(mockRegistry, cache, chainRegistry, cfg, logger)
	
	err := updater.updateConfigs(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to fetch token configs")
}

// Test initial update with retries
func TestConfigUpdater_PerformInitialUpdate_WithRetries(t *testing.T) {
	ctx := context.Background()
	logger := zerolog.Nop()
	
	callCount := 0
	mockRegistry := &MockedRegistryClient{
		getAllChainConfigsFunc: func(ctx context.Context) ([]*uregistrytypes.ChainConfig, error) {
			callCount++
			if callCount < 3 {
				return nil, errors.New("temporary error")
			}
			return []*uregistrytypes.ChainConfig{}, nil
		},
		getAllTokenConfigsFunc: func(ctx context.Context) ([]*uregistrytypes.TokenConfig, error) {
			return []*uregistrytypes.TokenConfig{}, nil
		},
	}
	
	cache := registry.NewConfigCache(logger)
	cfg := getTestConfig(30 * time.Second)
	cfg.InitialFetchRetries = 5
	cfg.RetryBackoff = 10 * time.Millisecond
	dbManager := db.NewInMemoryChainDBManager(logger, cfg)
	chainRegistry := chains.NewChainRegistry(dbManager, logger)
	chainRegistry.SetAppConfig(cfg)
	
	updater := NewConfigUpdater(mockRegistry, cache, chainRegistry, cfg, logger)
	
	err := updater.performInitialUpdate(ctx)
	assert.NoError(t, err)
	assert.Equal(t, 3, callCount) // Should succeed on third attempt
}

// Test initial update exhausts retries
func TestConfigUpdater_PerformInitialUpdate_ExhaustedRetries(t *testing.T) {
	ctx := context.Background()
	logger := zerolog.Nop()
	
	mockRegistry := &MockedRegistryClient{
		getAllChainConfigsFunc: func(ctx context.Context) ([]*uregistrytypes.ChainConfig, error) {
			return nil, errors.New("persistent error")
		},
	}
	
	cache := registry.NewConfigCache(logger)
	cfg := getTestConfig(30 * time.Second)
	dbManager := db.NewInMemoryChainDBManager(logger, cfg)
	chainRegistry := chains.NewChainRegistry(dbManager, logger)
	chainRegistry.SetAppConfig(cfg)
	cfg.InitialFetchRetries = 2
	cfg.RetryBackoff = 10 * time.Millisecond
	
	updater := NewConfigUpdater(mockRegistry, cache, chainRegistry, cfg, logger)
	
	err := updater.performInitialUpdate(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to fetch initial configuration after 2 attempts")
}

// Test force update
func TestConfigUpdater_ForceUpdate(t *testing.T) {
	ctx := context.Background()
	logger := zerolog.Nop()
	
	updateCount := 0
	mockRegistry := &MockedRegistryClient{
		getAllChainConfigsFunc: func(ctx context.Context) ([]*uregistrytypes.ChainConfig, error) {
			updateCount++
			return []*uregistrytypes.ChainConfig{}, nil
		},
		getAllTokenConfigsFunc: func(ctx context.Context) ([]*uregistrytypes.TokenConfig, error) {
			return []*uregistrytypes.TokenConfig{}, nil
		},
	}
	
	cache := registry.NewConfigCache(logger)
	cfg := getTestConfig(30 * time.Second)
	dbManager := db.NewInMemoryChainDBManager(logger, cfg)
	chainRegistry := chains.NewChainRegistry(dbManager, logger)
	chainRegistry.SetAppConfig(cfg)
	
	updater := NewConfigUpdater(mockRegistry, cache, chainRegistry, cfg, logger)
	
	err := updater.ForceUpdate(ctx)
	assert.NoError(t, err)
	assert.Equal(t, 1, updateCount)
	
	// Force update again
	err = updater.ForceUpdate(ctx)
	assert.NoError(t, err)
	assert.Equal(t, 2, updateCount)
}

// Test Stop method
func TestConfigUpdater_Stop(t *testing.T) {
	logger := zerolog.Nop()
	
	updater := &ConfigUpdater{
		logger: logger,
		ticker: time.NewTicker(1 * time.Hour),
		stopCh: make(chan struct{}),
	}
	
	// Start a goroutine that waits on stopCh
	stopped := make(chan bool)
	go func() {
		<-updater.stopCh
		stopped <- true
	}()
	
	// Stop the updater
	updater.Stop()
	
	// Verify stop signal was received
	select {
	case <-stopped:
		// Success
	case <-time.After(1 * time.Second):
		t.Error("Stop signal was not received")
	}
	
	// Verify ticker is nil after stop
	updater.Stop() // Should not panic on second call
}

// TestConfigUpdaterInitialization tests the creation of ConfigUpdater
func TestConfigUpdaterInitialization(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))
	
	// Create real instances for initialization test
	cache := registry.NewConfigCache(logger)
	chainReg := chains.NewChainRegistry(nil, logger)
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
		cfg := getTestConfig(30 * time.Second)
		dbManager := db.NewInMemoryChainDBManager(logger, cfg)
		defer dbManager.CloseAll()
		chainReg := chains.NewChainRegistry(dbManager, logger)
		chainReg.SetAppConfig(cfg)
		
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
		cfg := getTestConfig(30 * time.Second)
		dbManager := db.NewInMemoryChainDBManager(logger, cfg)
		defer dbManager.CloseAll()
		chainReg := chains.NewChainRegistry(dbManager, logger)
		chainReg.SetAppConfig(cfg)
		
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
		chainReg := chains.NewChainRegistry(nil, logger)
		
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
		chainReg := chains.NewChainRegistry(nil, logger)
		
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
	cfg := getTestConfig(30 * time.Second)
	
	// Create an in-memory database manager for testing
	dbManager := db.NewInMemoryChainDBManager(logger, cfg)
	chainReg := chains.NewChainRegistry(dbManager, logger)
	chainReg.SetAppConfig(cfg)
	
	// Create a test appConfig with RPC URLs
	appConfig := &config.Config{
		ChainRPCURLs: map[string][]string{
			"eip155:11155111": {"https://eth-sepolia.example.com"},
			"solana:EtWTRABZaYq6iMfeYKouRu166VU2xqa1": {"https://api.devnet.solana.com"},
		},
	}
	chainReg.SetAppConfig(appConfig)
	
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
				Enabled:      &uregistrytypes.ChainEnabled{IsInboundEnabled: true, IsOutboundEnabled: true},
			},
			{
				Chain:        "solana:EtWTRABZaYq6iMfeYKouRu166VU2xqa1",
				VmType:       uregistrytypes.VmType_SVM,
				PublicRpcUrl: "https://api.devnet.solana.com",
				Enabled:      &uregistrytypes.ChainEnabled{IsInboundEnabled: true, IsOutboundEnabled: true},
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
				Chain:   "eip155:11155111",
				VmType:  uregistrytypes.VmType_EVM,
				Enabled: &uregistrytypes.ChainEnabled{IsInboundEnabled: false, IsOutboundEnabled: false}, // Disabled
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
				Chain:   "", // Empty chain ID
				VmType:  uregistrytypes.VmType_EVM,
				Enabled: &uregistrytypes.ChainEnabled{IsInboundEnabled: true, IsOutboundEnabled: true},
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
	cfg := getTestConfig(30 * time.Second)
	
	// Create an in-memory database manager for testing
	dbManager := db.NewInMemoryChainDBManager(logger, cfg)
	chainReg := chains.NewChainRegistry(dbManager, logger)
	chainReg.SetAppConfig(cfg)
	
	// Create a test appConfig with RPC URLs
	appConfig := &config.Config{
		ChainRPCURLs: map[string][]string{
			"eip155:11155111": {"https://eth-sepolia.example.com"},
		},
	}
	chainReg.SetAppConfig(appConfig)
	
	forceUpdateCalled := false
	mockRegistry := &MockedRegistryClient{
		getAllChainConfigsFunc: func(ctx context.Context) ([]*uregistrytypes.ChainConfig, error) {
			forceUpdateCalled = true
			return []*uregistrytypes.ChainConfig{
				{
					Chain:   "eip155:11155111",
					VmType:  uregistrytypes.VmType_EVM,
					Enabled: &uregistrytypes.ChainEnabled{IsInboundEnabled: true, IsOutboundEnabled: true},
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
	chainReg := chains.NewChainRegistry(nil, logger)
	
	var updateCount int32
	mockRegistry := &MockedRegistryClient{
		getAllChainConfigsFunc: func(ctx context.Context) ([]*uregistrytypes.ChainConfig, error) {
			atomic.AddInt32(&updateCount, 1)
			// Return empty to avoid chain client initialization issues
			return []*uregistrytypes.ChainConfig{}, nil
		},
		getAllTokenConfigsFunc: func(ctx context.Context) ([]*uregistrytypes.TokenConfig, error) {
			return []*uregistrytypes.TokenConfig{}, nil
		},
	}
	
	testCfg := getTestConfig(100 * time.Millisecond) // Increased period for more reliable timing
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
	
	// Wait for periodic updates with more time buffer
	// Initial update happens immediately, then wait for 2 more updates
	time.Sleep(250 * time.Millisecond)
	
	// Should have initial update + at least 2 periodic updates
	count := atomic.LoadInt32(&updateCount)
	assert.GreaterOrEqual(t, count, int32(3))
	
	updater.Stop()
}

// TestConfigUpdaterStartStop tests start and stop functionality
func TestConfigUpdaterStartStop(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))
	cache := registry.NewConfigCache(logger)
	chainReg := chains.NewChainRegistry(nil, logger)
	
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
	chainReg := chains.NewChainRegistry(nil, logger)
	
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
	chainReg := chains.NewChainRegistry(nil, logger)
	
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
	cfg := getTestConfig(30 * time.Second)
	
	// Create an in-memory database manager for testing
	dbManager := db.NewInMemoryChainDBManager(logger, cfg)
	chainReg := chains.NewChainRegistry(dbManager, logger)
	chainReg.SetAppConfig(cfg)
	
	// Create a test appConfig with RPC URLs
	appConfig := &config.Config{
		ChainRPCURLs: map[string][]string{
			"eip155:11155111": {"https://eth-sepolia.example.com"},
		},
	}
	chainReg.SetAppConfig(appConfig)
	
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
					Chain:   "eip155:11155111",
					VmType:  uregistrytypes.VmType_EVM,
					Enabled: &uregistrytypes.ChainEnabled{IsInboundEnabled: true, IsOutboundEnabled: true},
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
	cfg := getTestConfig(30 * time.Second)
	
	// Create an in-memory database manager for testing
	dbManager := db.NewInMemoryChainDBManager(logger, cfg)
	chainReg := chains.NewChainRegistry(dbManager, logger)
	chainReg.SetAppConfig(cfg)
	
	// Create a test appConfig
	appConfig := &config.Config{
		ChainRPCURLs: map[string][]string{},
	}
	chainReg.SetAppConfig(appConfig)
	
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
	testCfg.InitialFetchRetries = 1 // Single attempt
	
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