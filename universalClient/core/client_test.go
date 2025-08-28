package core

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/rollchains/pchain/universalClient/config"
	"github.com/rollchains/pchain/universalClient/db"
	"github.com/rollchains/pchain/universalClient/logger"
	"github.com/stretchr/testify/assert"
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
