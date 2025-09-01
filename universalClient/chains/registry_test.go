package chains

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rollchains/pchain/universalClient/chains/common"
	"github.com/rollchains/pchain/universalClient/db"
	uregistrytypes "github.com/rollchains/pchain/x/uregistry/types"
)

// MockChainClient implements common.ChainClient for testing
type MockChainClient struct {
	config       *uregistrytypes.ChainConfig
	started      bool
	stopped      bool
	healthy      bool
	startError   error
	stopError    error
	mu           sync.Mutex
}

func NewMockChainClient(config *uregistrytypes.ChainConfig) *MockChainClient {
	return &MockChainClient{
		config:  config,
		healthy: true,
	}
}

func (m *MockChainClient) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	if m.startError != nil {
		return m.startError
	}
	m.started = true
	return nil
}

func (m *MockChainClient) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	if m.stopError != nil {
		return m.stopError
	}
	m.stopped = true
	m.started = false
	return nil
}

func (m *MockChainClient) IsHealthy() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.healthy && m.started
}

func (m *MockChainClient) GetConfig() *uregistrytypes.ChainConfig {
	return m.config
}

func (m *MockChainClient) ChainID() string {
	if m.config != nil {
		return m.config.Chain
	}
	return ""
}

func (m *MockChainClient) IsStarted() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.started
}

func (m *MockChainClient) IsStopped() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.stopped
}

// Implement GatewayOperations interface
func (m *MockChainClient) GetLatestBlock(ctx context.Context) (uint64, error) {
	return 0, nil
}

func (m *MockChainClient) WatchGatewayEvents(ctx context.Context, fromBlock uint64) (<-chan *common.GatewayEvent, error) {
	return nil, nil
}

func (m *MockChainClient) GetTransactionConfirmations(ctx context.Context, txHash string) (uint64, error) {
	return 0, nil
}

func (m *MockChainClient) IsConfirmed(ctx context.Context, txHash string) (bool, error) {
	return false, nil
}

// TestChainRegistryInitialization tests the creation of ChainRegistry
func TestChainRegistryInitialization(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))
	
	dbManager := db.NewInMemoryChainDBManager(logger, nil)
	defer dbManager.CloseAll()
	registry := NewChainRegistry(dbManager, logger)
	
	assert.NotNil(t, registry)
	assert.NotNil(t, registry.chains)
	assert.NotNil(t, registry.logger)
	assert.Len(t, registry.chains, 0)
}

// TestChainRegistryCreateChainClient tests chain client creation
func TestChainRegistryCreateChainClient(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))
	
	dbManager := db.NewInMemoryChainDBManager(logger, nil)
	defer dbManager.CloseAll()
	registry := NewChainRegistry(dbManager, logger)
	
	t.Run("Create EVM client", func(t *testing.T) {
		config := &uregistrytypes.ChainConfig{
			Chain:          "eip155:1",
			VmType:         uregistrytypes.VmType_EVM,
			GatewayAddress: "0x123...",
			Enabled: &uregistrytypes.ChainEnabled{IsInboundEnabled: true, IsOutboundEnabled: true},
		}
		
		client, err := registry.CreateChainClient(config)
		require.NoError(t, err)
		assert.NotNil(t, client)
		assert.Equal(t, config, client.GetConfig())
	})
	
	t.Run("Create Solana client", func(t *testing.T) {
		config := &uregistrytypes.ChainConfig{
			Chain:          "solana:mainnet",
			VmType:         uregistrytypes.VmType_SVM,
			GatewayAddress: "Sol123...",
			Enabled: &uregistrytypes.ChainEnabled{IsInboundEnabled: true, IsOutboundEnabled: true},
		}
		
		client, err := registry.CreateChainClient(config)
		require.NoError(t, err)
		assert.NotNil(t, client)
		assert.Equal(t, config, client.GetConfig())
	})
	
	t.Run("Nil config", func(t *testing.T) {
		client, err := registry.CreateChainClient(nil)
		assert.Error(t, err)
		assert.Nil(t, client)
		assert.Contains(t, err.Error(), "chain config is nil")
	})
	
	t.Run("Unsupported VM type", func(t *testing.T) {
		config := &uregistrytypes.ChainConfig{
			Chain:  "unknown:chain",
			VmType: 999, // Invalid VM type
		}
		
		client, err := registry.CreateChainClient(config)
		assert.Error(t, err)
		assert.Nil(t, client)
		assert.Contains(t, err.Error(), "unsupported VM type")
	})
}

// MockableChainRegistry extends ChainRegistry for testing
type MockableChainRegistry struct {
	*ChainRegistry
	createChainClientFunc func(*uregistrytypes.ChainConfig) (common.ChainClient, error)
}

func (m *MockableChainRegistry) CreateChainClient(config *uregistrytypes.ChainConfig) (common.ChainClient, error) {
	if m.createChainClientFunc != nil {
		return m.createChainClientFunc(config)
	}
	return m.ChainRegistry.CreateChainClient(config)
}

func (m *MockableChainRegistry) AddOrUpdateChain(ctx context.Context, config *uregistrytypes.ChainConfig) error {
	if config == nil || config.Chain == "" {
		return fmt.Errorf("invalid chain config")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	chainID := config.Chain

	// Check if chain already exists
	existingClient, exists := m.chains[chainID]
	if exists {
		// Check if configuration has changed
		existingConfig := existingClient.GetConfig()
		if configsEqual(existingConfig, config) {
			m.logger.Debug().
				Str("chain", chainID).
				Msg("chain config unchanged, skipping update")
			return nil
		}

		// Stop the existing client
		m.logger.Info().
			Str("chain", chainID).
			Msg("stopping existing chain client for update")
		if err := existingClient.Stop(); err != nil {
			m.logger.Error().
				Err(err).
				Str("chain", chainID).
				Msg("failed to stop existing chain client")
		}
		delete(m.chains, chainID)
	}

	// Create new chain client - this will use the overridden CreateChainClient
	client, err := m.CreateChainClient(config)
	if err != nil {
		return fmt.Errorf("failed to create chain client for %s: %w", chainID, err)
	}

	// Start the chain client
	if err := client.Start(ctx); err != nil {
		return fmt.Errorf("failed to start chain client for %s: %w", chainID, err)
	}

	m.chains[chainID] = client
	m.logger.Info().
		Str("chain", chainID).
		Msg("successfully added/updated chain client")

	return nil
}

// TestChainRegistryAddOrUpdateChain tests adding and updating chains
func TestChainRegistryAddOrUpdateChain(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))
	ctx := context.Background()
	
	t.Run("Add new chain - with mock", func(t *testing.T) {
		baseRegistry := &ChainRegistry{
			chains: make(map[string]common.ChainClient),
			logger: logger,
		}
		registry := &MockableChainRegistry{
			ChainRegistry: baseRegistry,
			createChainClientFunc: func(cfg *uregistrytypes.ChainConfig) (common.ChainClient, error) {
				return NewMockChainClient(cfg), nil
			},
		}
		
		config := &uregistrytypes.ChainConfig{
			Chain:          "eip155:1337",
			VmType:         uregistrytypes.VmType_EVM,
			GatewayAddress: "0x123...",
			Enabled: &uregistrytypes.ChainEnabled{IsInboundEnabled: true, IsOutboundEnabled: true},
		}
		
		err := registry.AddOrUpdateChain(ctx, config)
		require.NoError(t, err)
		
		// Verify chain was added
		client := registry.GetChain("eip155:1337")
		assert.NotNil(t, client)
		assert.True(t, client.(*MockChainClient).IsStarted())
	})
	
	t.Run("Update existing chain with same config", func(t *testing.T) {
		registry := &ChainRegistry{
			chains: make(map[string]common.ChainClient),
			logger: logger,
		}
		
		config := &uregistrytypes.ChainConfig{
			Chain:          "eip155:1337",
			VmType:         uregistrytypes.VmType_EVM,
			GatewayAddress: "0x123...",
			Enabled: &uregistrytypes.ChainEnabled{IsInboundEnabled: true, IsOutboundEnabled: true},
		}
		
		// Add initial client
		mockClient := NewMockChainClient(config)
		mockClient.started = true
		registry.chains["eip155:1337"] = mockClient
		
		// Try to update with same config
		err := registry.AddOrUpdateChain(ctx, config)
		require.NoError(t, err)
		
		// Verify client was not replaced (same instance)
		assert.Equal(t, mockClient, registry.GetChain("eip155:1337"))
		assert.False(t, mockClient.IsStopped()) // Should not have been stopped
	})
	
	t.Run("Update existing chain with different config", func(t *testing.T) {
		registry := &ChainRegistry{
			chains: make(map[string]common.ChainClient),
			logger: logger,
		}
		
		oldConfig := &uregistrytypes.ChainConfig{
			Chain:          "eip155:1337",
			VmType:         uregistrytypes.VmType_EVM,
			GatewayAddress: "0x123...",
			Enabled: &uregistrytypes.ChainEnabled{IsInboundEnabled: true, IsOutboundEnabled: true},
		}
		
		newConfig := &uregistrytypes.ChainConfig{
			Chain:          "eip155:1337",
			VmType:         uregistrytypes.VmType_EVM,
			GatewayAddress: "0x456...",
			Enabled: &uregistrytypes.ChainEnabled{IsInboundEnabled: true, IsOutboundEnabled: true},
		}
		
		// Add initial client
		oldClient := NewMockChainClient(oldConfig)
		oldClient.started = true
		registry.chains["eip155:1337"] = oldClient
		
		// Create a mockable registry
		mockableRegistry := &MockableChainRegistry{
			ChainRegistry: registry,
			createChainClientFunc: func(cfg *uregistrytypes.ChainConfig) (common.ChainClient, error) {
				return NewMockChainClient(cfg), nil
			},
		}
		
		// Update with new config
		err := mockableRegistry.AddOrUpdateChain(ctx, newConfig)
		require.NoError(t, err)
		
		// Verify old client was stopped
		assert.True(t, oldClient.IsStopped())
		
		// Verify new client was added
		newClient := registry.GetChain("eip155:1337")
		assert.NotNil(t, newClient)
		assert.NotEqual(t, oldClient, newClient)
		assert.Equal(t, newConfig, newClient.GetConfig())
	})
	
	t.Run("Invalid config", func(t *testing.T) {
		registry := NewChainRegistry(nil, logger)
		
		// Nil config
		err := registry.AddOrUpdateChain(ctx, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid chain config")
		
		// Empty chain ID
		err = registry.AddOrUpdateChain(ctx, &uregistrytypes.ChainConfig{
			Chain: "",
		})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid chain config")
	})
	
	t.Run("Client start error", func(t *testing.T) {
		registry := &ChainRegistry{
			chains: make(map[string]common.ChainClient),
			logger: logger,
		}
		
		config := &uregistrytypes.ChainConfig{
			Chain:  "eip155:1337",
			VmType: uregistrytypes.VmType_EVM,
		}
		
		// Create a mockable registry that returns client that fails to start
		mockableRegistry := &MockableChainRegistry{
			ChainRegistry: registry,
			createChainClientFunc: func(cfg *uregistrytypes.ChainConfig) (common.ChainClient, error) {
				mock := NewMockChainClient(cfg)
				mock.startError = errors.New("start failed")
				return mock, nil
			},
		}
		
		err := mockableRegistry.AddOrUpdateChain(ctx, config)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to start chain client")
		
		// Verify chain was not added
		assert.Nil(t, registry.GetChain("eip155:1337"))
	})
}

// TestChainRegistryRemoveChain tests removing chains
func TestChainRegistryRemoveChain(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))
	
	t.Run("Remove existing chain", func(t *testing.T) {
		registry := &ChainRegistry{
			chains: make(map[string]common.ChainClient),
			logger: logger,
		}
		
		// Add a chain
		mockClient := NewMockChainClient(&uregistrytypes.ChainConfig{
			Chain: "eip155:1337",
		})
		mockClient.started = true
		registry.chains["eip155:1337"] = mockClient
		
		// Remove it
		registry.RemoveChain("eip155:1337")
		
		// Verify it was removed
		assert.Nil(t, registry.GetChain("eip155:1337"))
		assert.True(t, mockClient.IsStopped())
	})
	
	t.Run("Remove non-existent chain", func(t *testing.T) {
		registry := NewChainRegistry(nil, logger)
		
		// Should not panic
		registry.RemoveChain("non-existent")
	})
	
	t.Run("Remove chain with stop error", func(t *testing.T) {
		registry := &ChainRegistry{
			chains: make(map[string]common.ChainClient),
			logger: logger,
		}
		
		// Add a chain that fails to stop
		mockClient := NewMockChainClient(&uregistrytypes.ChainConfig{
			Chain: "eip155:1337",
		})
		mockClient.stopError = errors.New("stop failed")
		registry.chains["eip155:1337"] = mockClient
		
		// Remove it (should log error but still remove)
		registry.RemoveChain("eip155:1337")
		
		// Verify it was removed despite error
		assert.Nil(t, registry.GetChain("eip155:1337"))
	})
}

// TestChainRegistryGetChain tests retrieving chains
func TestChainRegistryGetChain(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))
	registry := &ChainRegistry{
		chains: make(map[string]common.ChainClient),
		logger: logger,
	}
	
	// Add a chain
	mockClient := NewMockChainClient(&uregistrytypes.ChainConfig{
		Chain: "eip155:1337",
	})
	registry.chains["eip155:1337"] = mockClient
	
	t.Run("Get existing chain", func(t *testing.T) {
		client := registry.GetChain("eip155:1337")
		assert.NotNil(t, client)
		assert.Equal(t, mockClient, client)
	})
	
	t.Run("Get non-existent chain", func(t *testing.T) {
		client := registry.GetChain("non-existent")
		assert.Nil(t, client)
	})
}

// TestChainRegistryGetAllChains tests getting all chains
func TestChainRegistryGetAllChains(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))
	registry := &ChainRegistry{
		chains: make(map[string]common.ChainClient),
		logger: logger,
	}
	
	// Add multiple chains
	chain1 := NewMockChainClient(&uregistrytypes.ChainConfig{Chain: "test:chain1"})
	chain2 := NewMockChainClient(&uregistrytypes.ChainConfig{Chain: "eip155:1338"})
	registry.chains["eip155:1337"] = chain1
	registry.chains["eip155:1338"] = chain2
	
	chains := registry.GetAllChains()
	assert.Len(t, chains, 2)
	assert.Equal(t, chain1, chains["eip155:1337"])
	assert.Equal(t, chain2, chains["eip155:1338"])
	
	// Verify it's a copy (modifications don't affect registry)
	delete(chains, "test:chain1")
	assert.Len(t, registry.chains, 2)
}

// TestChainRegistryStopAll tests stopping all chains
func TestChainRegistryStopAll(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))
	registry := &ChainRegistry{
		chains: make(map[string]common.ChainClient),
		logger: logger,
	}
	
	// Add multiple chains
	chain1 := NewMockChainClient(&uregistrytypes.ChainConfig{Chain: "test:chain1"})
	chain2 := NewMockChainClient(&uregistrytypes.ChainConfig{Chain: "eip155:1338"})
	chain1.started = true
	chain2.started = true
	registry.chains["eip155:1337"] = chain1
	registry.chains["eip155:1338"] = chain2
	
	// Stop all
	registry.StopAll()
	
	// Verify all stopped and registry cleared
	assert.True(t, chain1.IsStopped())
	assert.True(t, chain2.IsStopped())
	assert.Len(t, registry.chains, 0)
}

// TestChainRegistryGetHealthStatus tests health status retrieval
func TestChainRegistryGetHealthStatus(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))
	registry := &ChainRegistry{
		chains: make(map[string]common.ChainClient),
		logger: logger,
	}
	
	// Add chains with different health states
	healthyChain := NewMockChainClient(&uregistrytypes.ChainConfig{Chain: "eip155:1001"})
	unhealthyChain := NewMockChainClient(&uregistrytypes.ChainConfig{Chain: "eip155:1002"})
	stoppedChain := NewMockChainClient(&uregistrytypes.ChainConfig{Chain: "eip155:1003"})
	
	healthyChain.started = true
	healthyChain.healthy = true
	
	unhealthyChain.started = true
	unhealthyChain.healthy = false
	
	stoppedChain.started = false
	stoppedChain.healthy = true
	
	registry.chains["eip155:1001"] = healthyChain
	registry.chains["eip155:1002"] = unhealthyChain
	registry.chains["eip155:1003"] = stoppedChain
	
	status := registry.GetHealthStatus()
	assert.Len(t, status, 3)
	assert.True(t, status["eip155:1001"])
	assert.False(t, status["eip155:1002"])
	assert.False(t, status["eip155:1003"]) // Not healthy because not started
}

// TestChainRegistryConcurrency tests concurrent operations
func TestChainRegistryConcurrency(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))
	registry := &ChainRegistry{
		chains: make(map[string]common.ChainClient),
		logger: logger,
	}
	
	// Create a mockable registry for concurrent adds
	mockableRegistry := &MockableChainRegistry{
		ChainRegistry: registry,
		createChainClientFunc: func(cfg *uregistrytypes.ChainConfig) (common.ChainClient, error) {
			return NewMockChainClient(cfg), nil
		},
	}
	
	ctx := context.Background()
	var wg sync.WaitGroup
	
	// Concurrent adds
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			config := &uregistrytypes.ChainConfig{
				Chain:  fmt.Sprintf("eip155:%d", 2000+id),
				VmType: uregistrytypes.VmType_EVM,
			}
			_ = mockableRegistry.AddOrUpdateChain(ctx, config)
		}(i)
	}
	
	// Concurrent reads
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = registry.GetAllChains()
			_ = registry.GetHealthStatus()
		}()
	}
	
	// Concurrent removes
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			time.Sleep(10 * time.Millisecond) // Let adds happen first
			registry.RemoveChain(fmt.Sprintf("eip155:%d", 2000+id))
		}(i)
	}
	
	wg.Wait()
	
	// Verify state is consistent
	chains := registry.GetAllChains()
	assert.GreaterOrEqual(t, len(chains), 5) // At least 5 should remain
}

// TestConfigsEqual tests the configsEqual helper function
func TestConfigsEqual(t *testing.T) {
	config1 := &uregistrytypes.ChainConfig{
		Chain:          "eip155:1337",
		VmType:         uregistrytypes.VmType_EVM,
		GatewayAddress: "0x123",
		Enabled: &uregistrytypes.ChainEnabled{IsInboundEnabled: true, IsOutboundEnabled: true},
	}
	
	config2 := &uregistrytypes.ChainConfig{
		Chain:          "eip155:1337",
		VmType:         uregistrytypes.VmType_EVM,
		GatewayAddress: "0x123",
		Enabled: &uregistrytypes.ChainEnabled{IsInboundEnabled: true, IsOutboundEnabled: true},
	}
	
	// Config with different gateway address
	config3 := &uregistrytypes.ChainConfig{
		Chain:          "eip155:1337",
		VmType:         uregistrytypes.VmType_EVM,
		GatewayAddress: "0x456",
		Enabled: &uregistrytypes.ChainEnabled{IsInboundEnabled: true, IsOutboundEnabled: true},
	}
	
	t.Run("Equal configs", func(t *testing.T) {
		assert.True(t, configsEqual(config1, config2))
	})
	
	t.Run("Different configs", func(t *testing.T) {
		assert.False(t, configsEqual(config1, config3))
	})
	
	t.Run("Nil configs", func(t *testing.T) {
		assert.True(t, configsEqual(nil, nil))
		assert.False(t, configsEqual(config1, nil))
		assert.False(t, configsEqual(nil, config1))
	})
}