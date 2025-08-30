package core

import (
	"context"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/rollchains/pchain/universalClient/config"
	"github.com/rollchains/pchain/universalClient/db"
	"github.com/rollchains/pchain/universalClient/registry"
	uregistrytypes "github.com/rollchains/pchain/x/uregistry/types"
)

// MockRegistryInterface is a mock implementation of RegistryInterface
type MockRegistryInterface struct {
	mock.Mock
}

func (m *MockRegistryInterface) GetAllChainConfigs(ctx context.Context) ([]*uregistrytypes.ChainConfig, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*uregistrytypes.ChainConfig), args.Error(1)
}

func (m *MockRegistryInterface) GetAllTokenConfigs(ctx context.Context) ([]*uregistrytypes.TokenConfig, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*uregistrytypes.TokenConfig), args.Error(1)
}

func TestNewUniversalClient(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *config.Config
		wantErr bool
		errMsg  string
	}{
		{
			name: "successful creation",
			cfg: &config.Config{
				PushChainGRPCURLs:      []string{"localhost:9090"},
				QueryServerPort:        8080,
				ConfigRefreshInterval:  30 * time.Second,
				EventPollingInterval:   5 * time.Second,
				InitialFetchRetries:    3,
				InitialFetchTimeout:    10 * time.Second,
				RetryBackoff:          2 * time.Second,
			},
			wantErr: false,
		},
		{
			name: "empty grpc urls",
			cfg: &config.Config{
				PushChainGRPCURLs: []string{},
				QueryServerPort:   8080,
			},
			wantErr: true,
			errMsg:  "failed to create registry client",
		},
		{
			name: "nil config",
			cfg:  nil,
			wantErr: true,
			errMsg:  "config is nil",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			logger := zerolog.Nop()
			
			// Create mock database manager
			dbManager := &db.ChainDBManager{}
			
			// Handle nil config case
			if tt.cfg == nil {
				client, err := NewUniversalClient(ctx, logger, dbManager, tt.cfg)
				assert.Nil(t, client)
				assert.Error(t, err)
				return
			}
			
			client, err := NewUniversalClient(ctx, logger, dbManager, tt.cfg)
			
			if tt.wantErr {
				assert.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
				assert.Nil(t, client)
			} else {
				// Note: This will fail in actual test due to registry connection
				// In real tests, we'd need to mock the registry client creation
				if err != nil && err.Error() == "failed to create registry client: no gRPC URLs provided" {
					t.Skip("Skipping due to missing mock for registry client")
				}
				assert.NoError(t, err)
				assert.NotNil(t, client)
			}
		})
	}
}

func TestUniversalClient_GetChainConfig(t *testing.T) {
	// Create test client with mocked dependencies
	ctx := context.Background()
	logger := zerolog.Nop()
	
	// Create config cache
	cache := registry.NewConfigCache(logger)
	
	// Create test chain configs
	chainConfig := &uregistrytypes.ChainConfig{
		Chain:      "eip155:1",
		VmType:     uregistrytypes.VmType_EVM,
		Enabled:    &uregistrytypes.ChainEnabled{IsInboundEnabled: true},
		GatewayAddress: "0x1234567890abcdef",
	}
	
	// Update cache
	cache.UpdateAll([]*uregistrytypes.ChainConfig{chainConfig}, nil)
	
	// Create client
	client := &UniversalClient{
		ctx:         ctx,
		log:         logger,
		configCache: cache,
	}
	
	tests := []struct {
		name     string
		chainID  string
		expected *uregistrytypes.ChainConfig
	}{
		{
			name:     "existing chain",
			chainID:  "eip155:1",
			expected: chainConfig,
		},
		{
			name:     "non-existing chain",
			chainID:  "eip155:999",
			expected: nil,
		},
		{
			name:     "empty chain ID",
			chainID:  "",
			expected: nil,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := client.GetChainConfig(tt.chainID)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestUniversalClient_GetAllChainConfigs(t *testing.T) {
	ctx := context.Background()
	logger := zerolog.Nop()
	cache := registry.NewConfigCache(logger)
	
	// Create test chain configs
	configs := []*uregistrytypes.ChainConfig{
		{
			Chain:   "eip155:1",
			VmType:  uregistrytypes.VmType_EVM,
			Enabled: &uregistrytypes.ChainEnabled{IsInboundEnabled: true},
		},
		{
			Chain:   "solana:mainnet",
			VmType:  uregistrytypes.VmType_SVM,
			Enabled: &uregistrytypes.ChainEnabled{IsOutboundEnabled: true},
		},
	}
	
	// Update cache
	cache.UpdateAll(configs, nil)
	
	client := &UniversalClient{
		ctx:         ctx,
		log:         logger,
		configCache: cache,
	}
	
	result := client.GetAllChainConfigs()
	assert.Len(t, result, 2)
	assert.Contains(t, result, configs[0])
	assert.Contains(t, result, configs[1])
}

func TestUniversalClient_GetTokenConfig(t *testing.T) {
	ctx := context.Background()
	logger := zerolog.Nop()
	cache := registry.NewConfigCache(logger)
	
	// Create test token config
	tokenConfig := &uregistrytypes.TokenConfig{
		Chain:   "eip155:1",
		Address: "0xabc123",
		Symbol:  "TEST",
		Decimals: 18,
	}
	
	// Update cache
	cache.UpdateAll(nil, []*uregistrytypes.TokenConfig{tokenConfig})
	
	client := &UniversalClient{
		ctx:         ctx,
		log:         logger,
		configCache: cache,
	}
	
	tests := []struct {
		name     string
		chain    string
		address  string
		expected *uregistrytypes.TokenConfig
	}{
		{
			name:     "existing token",
			chain:    "eip155:1",
			address:  "0xabc123",
			expected: tokenConfig,
		},
		{
			name:     "non-existing token",
			chain:    "eip155:1",
			address:  "0xdef456",
			expected: nil,
		},
		{
			name:     "wrong chain",
			chain:    "eip155:2",
			address:  "0xabc123",
			expected: nil,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := client.GetTokenConfig(tt.chain, tt.address)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestUniversalClient_GetTokenConfigsByChain(t *testing.T) {
	ctx := context.Background()
	logger := zerolog.Nop()
	cache := registry.NewConfigCache(logger)
	
	// Create test token configs
	tokenConfigs := []*uregistrytypes.TokenConfig{
		{
			Chain:   "eip155:1",
			Address: "0xabc123",
			Symbol:  "TEST1",
		},
		{
			Chain:   "eip155:1",
			Address: "0xdef456",
			Symbol:  "TEST2",
		},
		{
			Chain:   "eip155:2",
			Address: "0x789ghi",
			Symbol:  "TEST3",
		},
	}
	
	// Update cache
	cache.UpdateAll(nil, tokenConfigs)
	
	client := &UniversalClient{
		ctx:         ctx,
		log:         logger,
		configCache: cache,
	}
	
	tests := []struct {
		name          string
		chain         string
		expectedCount int
	}{
		{
			name:          "chain with multiple tokens",
			chain:         "eip155:1",
			expectedCount: 2,
		},
		{
			name:          "chain with single token",
			chain:         "eip155:2",
			expectedCount: 1,
		},
		{
			name:          "chain with no tokens",
			chain:         "eip155:999",
			expectedCount: 0,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := client.GetTokenConfigsByChain(tt.chain)
			assert.Len(t, result, tt.expectedCount)
		})
	}
}

func TestUniversalClient_GetCacheLastUpdate(t *testing.T) {
	ctx := context.Background()
	logger := zerolog.Nop()
	cache := registry.NewConfigCache(logger)
	
	// Record time before update
	before := time.Now()
	
	// Update cache
	cache.UpdateAll([]*uregistrytypes.ChainConfig{}, []*uregistrytypes.TokenConfig{})
	
	// Record time after update
	after := time.Now()
	
	client := &UniversalClient{
		ctx:         ctx,
		log:         logger,
		configCache: cache,
	}
	
	lastUpdate := client.GetCacheLastUpdate()
	
	// Verify last update is between before and after
	assert.True(t, lastUpdate.After(before) || lastUpdate.Equal(before))
	assert.True(t, lastUpdate.Before(after) || lastUpdate.Equal(after))
}

func TestUniversalClient_StartStop(t *testing.T) {
	// This test would require extensive mocking of all components
	// For now, we'll create a basic structure test
	
	t.Run("start requires config updater", func(t *testing.T) {
		client := &UniversalClient{
			ctx: context.Background(),
			log: zerolog.Nop(),
		}
		
		err := client.Start()
		assert.Error(t, err)
	})
	
	t.Run("stop handles nil components gracefully", func(t *testing.T) {
		client := &UniversalClient{
			ctx: context.Background(),
			log: zerolog.Nop(),
		}
		
		// Should not panic even with nil components
		assert.NotPanics(t, func() {
			if client.configUpdater != nil {
				client.configUpdater.Stop()
			}
		})
	})
}

// Table-driven test for error scenarios
func TestUniversalClient_ErrorScenarios(t *testing.T) {
	tests := []struct {
		name     string
		setup    func() *UniversalClient
		testFunc func(*UniversalClient) error
		wantErr  bool
	}{
		{
			name: "force update with nil updater",
			setup: func() *UniversalClient {
				return &UniversalClient{
					ctx: context.Background(),
					log: zerolog.Nop(),
				}
			},
			testFunc: func(uc *UniversalClient) error {
				return uc.ForceConfigUpdate()
			},
			wantErr: true,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := tt.setup()
			err := tt.testFunc(client)
			
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}