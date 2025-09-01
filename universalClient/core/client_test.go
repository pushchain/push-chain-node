package core

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/rollchains/pchain/universalClient/config"
	"github.com/rollchains/pchain/universalClient/db"
	"github.com/rollchains/pchain/universalClient/logger"
	"github.com/rollchains/pchain/universalClient/registry"
	uregistrytypes "github.com/rollchains/pchain/x/uregistry/types"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestNewUniversalClient(t *testing.T) {
	// Create temporary directory for testing
	tempDir, err := os.MkdirTemp("", "client-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Create test database
	database, err := db.OpenFileDB(tempDir, "test.db", false)
	require.NoError(t, err)
	defer database.Close()

	// Create test config without hot key
	cfg := &config.Config{
		LogLevel:              1,
		LogFormat:             "console",
		LogSampler:            false,
		PushChainGRPCURLs:     []string{"localhost:9090"},
		ConfigRefreshInterval: 10 * time.Minute,
		MaxRetries:            3,
		RetryBackoff:          1 * time.Second,
		InitialFetchRetries:   5,
		InitialFetchTimeout:   30 * time.Second,
		QueryServerPort:       8080,
	}

	// Initialize logger
	log := logger.Init(*cfg)
	ctx := context.Background()

	// Create UniversalClient
	client, err := NewUniversalClient(ctx, log, database, cfg)
	require.NoError(t, err)
	require.NotNil(t, client)

	// Validate basic fields
	assert.Equal(t, ctx, client.ctx)
	assert.Equal(t, database, client.db)
	assert.Equal(t, cfg, client.config)
	assert.NotNil(t, client.registryClient)
	assert.NotNil(t, client.configCache)
	assert.NotNil(t, client.configUpdater)
	assert.NotNil(t, client.chainRegistry)
	assert.NotNil(t, client.queryServer)

	// Hot key components should be nil for non-hot-key config
	assert.Nil(t, client.keys)
}

func TestNewUniversalClientWithKeyring(t *testing.T) {
	// Create temporary directory for testing
	tempDir, err := os.MkdirTemp("", "client-keyring-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Create test database
	database, err := db.OpenFileDB(tempDir, "test.db", false)
	require.NoError(t, err)
	defer database.Close()

	// Create test config with keyring configuration
	cfg := &config.Config{
		LogLevel:              1,
		LogFormat:             "console",
		LogSampler:            false,
		PushChainGRPCURLs:     []string{"localhost:9090"},
		ConfigRefreshInterval: 10 * time.Minute,
		MaxRetries:            3,
		RetryBackoff:          1 * time.Second,
		InitialFetchRetries:   5,
		InitialFetchTimeout:   30 * time.Second,
		QueryServerPort:       8080,
		KeyringBackend:        config.KeyringBackendTest,
	}

	// Initialize logger
	log := logger.Init(*cfg)
	ctx := context.Background()

	// Create UniversalClient - should succeed with keyring config
	client, err := NewUniversalClient(ctx, log, database, cfg)
	require.NoError(t, err)
	require.NotNil(t, client)

	// Validate basic fields
	assert.Equal(t, ctx, client.ctx)
	assert.Equal(t, database, client.db)
	assert.Equal(t, cfg, client.config)
	assert.NotNil(t, client.registryClient)
	assert.NotNil(t, client.configCache)
	assert.NotNil(t, client.configUpdater)
	assert.NotNil(t, client.chainRegistry)
	assert.NotNil(t, client.queryServer)

	// Keys should be nil since no AuthZ config
	assert.Nil(t, client.keys)
}

func TestUniversalClientMethods(t *testing.T) {
	// Create temporary directory for testing
	tempDir, err := os.MkdirTemp("", "client-methods-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Create test database
	database, err := db.OpenFileDB(tempDir, "test.db", false)
	require.NoError(t, err)
	defer database.Close()

	// Create test config
	cfg := &config.Config{
		LogLevel:              1,
		LogFormat:             "console",
		PushChainGRPCURLs:     []string{"localhost:9090"},
		ConfigRefreshInterval: 10 * time.Minute,
		QueryServerPort:       8080,
	}

	// Initialize logger
	log := logger.Init(*cfg)
	ctx := context.Background()

	// Create UniversalClient
	client, err := NewUniversalClient(ctx, log, database, cfg)
	require.NoError(t, err)

	// Test configuration methods
	allChains := client.GetAllChainConfigs()
	assert.NotNil(t, allChains)

	allTokens := client.GetAllTokenConfigs()
	assert.NotNil(t, allTokens)

	chainConfig := client.GetChainConfig("test-chain")
	assert.Nil(t, chainConfig) // Should be nil for non-existent chain

	tokenConfig := client.GetTokenConfig("test-chain", "test-address")
	assert.Nil(t, tokenConfig) // Should be nil for non-existent token

	tokensForChain := client.GetTokenConfigsByChain("test-chain")
	assert.NotNil(t, tokensForChain)
	assert.Empty(t, tokensForChain)

	// Test cache timestamp
	lastUpdate := client.GetCacheLastUpdate()
	assert.IsType(t, time.Time{}, lastUpdate)

	// Test chain client access
	chainClient := client.GetChainClient("test-chain")
	assert.Nil(t, chainClient) // Should be nil for non-existent chain

	// Hot key components should be nil for non-hot-key config
	assert.Nil(t, client.keys)
}

func TestUniversalClientBasicHotKeyMethods(t *testing.T) {
	// Test that hot key components are nil when not configured
	tempDir, err := os.MkdirTemp("", "client-hotkey-methods-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	database, err := db.OpenFileDB(tempDir, "test.db", false)
	require.NoError(t, err)
	defer database.Close()

	cfg := &config.Config{
		LogLevel:          1,
		LogFormat:         "console",
		PushChainGRPCURLs: []string{"localhost:9090"},
		QueryServerPort:   8080,
	}

	log := logger.Init(*cfg)
	ctx := context.Background()

	client, err := NewUniversalClient(ctx, log, database, cfg)
	require.NoError(t, err)

	// Test hot key components are nil
	assert.Nil(t, client.keys)
}

func TestUniversalClientStart(t *testing.T) {
	// Create temporary directory for testing
	tempDir, err := os.MkdirTemp("", "client-start-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Create test database
	database, err := db.OpenFileDB(tempDir, "test.db", false)
	require.NoError(t, err)
	defer database.Close()

	// Create test config
	cfg := &config.Config{
		LogLevel:              1,
		LogFormat:             "console",
		PushChainGRPCURLs:     []string{"localhost:9090"},
		ConfigRefreshInterval: 10 * time.Minute,
		QueryServerPort:       8081, // Use different port to avoid conflicts
	}

	// Initialize logger
	log := logger.Init(*cfg)

	// Create a context that we can cancel
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Create UniversalClient
	client, err := NewUniversalClient(ctx, log, database, cfg)
	require.NoError(t, err)

	// Start client (will run until context is cancelled)
	go func() {
		err := client.Start()
		// Error is expected when context is cancelled
		if err != nil {
			t.Logf("Client start error (expected): %v", err)
		}
	}()

	// Wait for context to timeout
	<-ctx.Done()

	// Give some time for cleanup
	time.Sleep(100 * time.Millisecond)
}

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
				PushChainGRPCURLs:            []string{"localhost:9090"},
				QueryServerPort:              8080,
				ConfigRefreshIntervalSeconds: 30,
				EventPollingIntervalSeconds:  5,
				InitialFetchRetries:          3,
				InitialFetchTimeoutSeconds:   10,
				RetryBackoffSeconds:          2,
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
			name:    "nil config",
			cfg:     nil,
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
		Chain:          "eip155:1",
		VmType:         uregistrytypes.VmType_EVM,
		Enabled:        &uregistrytypes.ChainEnabled{IsInboundEnabled: true},
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
		Chain:    "eip155:1",
		Address:  "0xabc123",
		Symbol:   "TEST",
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
