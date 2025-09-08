package evm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/universalClient/config"
	uregistrytypes "github.com/pushchain/push-chain-node/x/uregistry/types"
)

// testAppConfig creates a test app config with RPC URLs
func testAppConfig(chainID string, rpcURLs []string) *config.Config {
	return &config.Config{
		ChainConfigs: map[string]config.ChainSpecificConfig{
			chainID: {
				RPCURLs: rpcURLs,
			},
		},
	}
}

// TestClientInitialization tests the creation of EVM client
func TestClientInitialization(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))

	t.Run("Valid config", func(t *testing.T) {
		config := &uregistrytypes.ChainConfig{
			Chain:          "eip155:1",
			VmType:         uregistrytypes.VmType_EVM,
			GatewayAddress: "0x123...",
			Enabled:        &uregistrytypes.ChainEnabled{IsInboundEnabled: true, IsOutboundEnabled: true},
		}

		client, err := NewClient(config, nil, nil, logger)
		require.NoError(t, err)
		assert.NotNil(t, client)
		assert.Equal(t, int64(1), client.chainID)
		assert.Equal(t, config, client.GetConfig())
		assert.Equal(t, "eip155:1", client.ChainID())
	})

	t.Run("Nil config", func(t *testing.T) {
		client, err := NewClient(nil, nil, nil, logger)
		assert.Error(t, err)
		assert.Nil(t, client)
		assert.Contains(t, err.Error(), "config is nil")
	})

	t.Run("Invalid VM type", func(t *testing.T) {
		config := &uregistrytypes.ChainConfig{
			Chain:  "eip155:1",
			VmType: uregistrytypes.VmType_SVM, // Wrong VM type
		}

		client, err := NewClient(config, nil, nil, logger)
		assert.Error(t, err)
		assert.Nil(t, client)
		assert.Contains(t, err.Error(), "invalid VM type for EVM client")
	})

	t.Run("Invalid chain ID format", func(t *testing.T) {
		config := &uregistrytypes.ChainConfig{
			Chain:  "invalid:format",
			VmType: uregistrytypes.VmType_EVM,
		}

		client, err := NewClient(config, nil, nil, logger)
		assert.Error(t, err)
		assert.Nil(t, client)
		assert.Contains(t, err.Error(), "not an EVM chain")
	})

	t.Run("Invalid chain ID number", func(t *testing.T) {
		config := &uregistrytypes.ChainConfig{
			Chain:  "eip155:abc",
			VmType: uregistrytypes.VmType_EVM,
		}

		client, err := NewClient(config, nil, nil, logger)
		assert.Error(t, err)
		assert.Nil(t, client)
		assert.Contains(t, err.Error(), "failed to parse chain ID")
	})
}

// TestParseEVMChainID tests the CAIP-2 chain ID parsing
func TestParseEVMChainID(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		expected  int64
		expectErr bool
		errMsg    string
	}{
		{
			name:     "Valid mainnet",
			input:    "eip155:1",
			expected: 1,
		},
		{
			name:     "Valid Sepolia",
			input:    "eip155:11155111",
			expected: 11155111,
		},
		{
			name:      "Invalid format - missing colon",
			input:     "eip1551",
			expectErr: true,
			errMsg:    "invalid CAIP-2 format",
		},
		{
			name:      "Invalid format - too many parts",
			input:     "eip155:1:extra",
			expectErr: true,
			errMsg:    "invalid CAIP-2 format",
		},
		{
			name:      "Wrong namespace",
			input:     "solana:1",
			expectErr: true,
			errMsg:    "not an EVM chain",
		},
		{
			name:      "Non-numeric chain ID",
			input:     "eip155:mainnet",
			expectErr: true,
			errMsg:    "failed to parse chain ID",
		},
		{
			name:     "Large chain ID",
			input:    "eip155:999999999",
			expected: 999999999,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseEVMChainID(tt.input)

			if tt.expectErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

// TestClientStartStop tests starting and stopping the client
func TestClientStartStop(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))

	t.Run("Start with mock server", func(t *testing.T) {
		// Create a mock HTTP server
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Mock eth_chainId response
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":"0x1"}`))
		}))
		defer server.Close()

		config := &uregistrytypes.ChainConfig{
			Chain:          "eip155:1",
			VmType:         uregistrytypes.VmType_EVM,
			GatewayAddress: "0x123...",
			Enabled:        &uregistrytypes.ChainEnabled{IsInboundEnabled: true, IsOutboundEnabled: true},
		}

		appConfig := testAppConfig("eip155:1", []string{server.URL})
		client, err := NewClient(config, nil, appConfig, logger)
		require.NoError(t, err)

		ctx := context.Background()
		err = client.Start(ctx)
		assert.NoError(t, err)
		assert.NotNil(t, client.ethClient)

		// Test Stop
		err = client.Stop()
		assert.NoError(t, err)
		assert.Nil(t, client.ethClient)
	})

	t.Run("Start with invalid URL", func(t *testing.T) {
		config := &uregistrytypes.ChainConfig{
			Chain:          "eip155:1",
			VmType:         uregistrytypes.VmType_EVM,
			GatewayAddress: "0x123...",
		}

		appConfig := testAppConfig("eip155:1", []string{"http://invalid.localhost:99999"})
		// Reduce timeout for tests to fail faster
		appConfig.RPCPoolConfig.RequestTimeoutSeconds = 2 // Reduce from default 30s to 2s
		
		client, err := NewClient(config, nil, appConfig, logger)
		require.NoError(t, err)

		// Use context with timeout to ensure fast failure
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		
		err = client.Start(ctx)
		assert.Error(t, err)
	})

	t.Run("Start with context cancellation", func(t *testing.T) {
		config := &uregistrytypes.ChainConfig{
			Chain:  "eip155:1",
			VmType: uregistrytypes.VmType_EVM,
		}

		// Empty URL will cause connection to fail
		appConfig := testAppConfig("eip155:1", []string{})
		client, err := NewClient(config, nil, appConfig, logger)
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		err = client.Start(ctx)
		assert.Error(t, err)
	})
}

// TestClientIsHealthy tests the health check functionality
func TestClientIsHealthy(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))

	t.Run("Healthy client", func(t *testing.T) {
		// Create a mock HTTP server that handles both eth_chainId and eth_blockNumber
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Read the request body to determine which method is being called
			var response string
			if r.Body != nil {
				defer r.Body.Close()
				body := make([]byte, 1024)
				n, _ := r.Body.Read(body)
				bodyStr := string(body[:n])
				if strings.Contains(bodyStr, "eth_chainId") {
					// Return chain ID 1 (0x1)
					response = `{"jsonrpc":"2.0","id":1,"result":"0x1"}`
				} else if strings.Contains(bodyStr, "eth_blockNumber") {
					// Return some block number
					response = `{"jsonrpc":"2.0","id":1,"result":"0x10"}`
				} else {
					// Default response
					response = `{"jsonrpc":"2.0","id":1,"result":"0x1"}`
				}
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(response))
		}))
		defer server.Close()

		config := &uregistrytypes.ChainConfig{
			Chain:  "eip155:1",
			VmType: uregistrytypes.VmType_EVM,
		}

		appConfig := testAppConfig("eip155:1", []string{server.URL})
		client, err := NewClient(config, nil, appConfig, logger)
		require.NoError(t, err)

		// Start the client
		ctx := context.Background()
		err = client.Start(ctx)
		require.NoError(t, err)

		// Check health
		healthy := client.IsHealthy()
		assert.True(t, healthy)

		// Stop the client
		client.Stop()
	})

	t.Run("Not healthy - not started", func(t *testing.T) {
		config := &uregistrytypes.ChainConfig{
			Chain:  "eip155:1",
			VmType: uregistrytypes.VmType_EVM,
		}

		client, err := NewClient(config, nil, nil, logger)
		require.NoError(t, err)

		healthy := client.IsHealthy()
		assert.False(t, healthy)
	})
}

// TestClientGetMethods tests getter methods
func TestClientGetMethods(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))

	config := &uregistrytypes.ChainConfig{
		Chain:          "eip155:11155111",
		VmType:         uregistrytypes.VmType_EVM,
		GatewayAddress: "0x123...",
		Enabled:        &uregistrytypes.ChainEnabled{IsInboundEnabled: true, IsOutboundEnabled: true},
	}

	appConfig := testAppConfig("eip155:11155111", []string{"https://eth-sepolia.example.com"})
	client, err := NewClient(config, nil, appConfig, logger)
	require.NoError(t, err)

	t.Run("GetChainID", func(t *testing.T) {
		assert.Equal(t, int64(11155111), client.GetChainID())
	})

	t.Run("GetRPCURL", func(t *testing.T) {
		assert.Equal(t, "https://eth-sepolia.example.com", client.GetRPCURL())
	})

	t.Run("ChainID", func(t *testing.T) {
		assert.Equal(t, "eip155:11155111", client.ChainID())
	})

	t.Run("GetConfig", func(t *testing.T) {
		assert.Equal(t, config, client.GetConfig())
	})
}

// TestClientGetLatestBlockNumber tests block number retrieval
func TestClientGetLatestBlockNumber(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))

	t.Run("Success", func(t *testing.T) {
		// Create a mock HTTP server
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var response string
			if r.Body != nil {
				defer r.Body.Close()
				body := make([]byte, 1024)
				n, _ := r.Body.Read(body)
				bodyStr := string(body[:n])
				if strings.Contains(bodyStr, "eth_chainId") {
					// Return chain ID 1 (0x1)
					response = `{"jsonrpc":"2.0","id":1,"result":"0x1"}`
				} else if strings.Contains(bodyStr, "eth_blockNumber") {
					// Return block number
					response = `{"jsonrpc":"2.0","id":1,"result":"0x1234"}`
				} else {
					response = `{"jsonrpc":"2.0","id":1,"result":"0x1"}`
				}
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(response))
		}))
		defer server.Close()

		config := &uregistrytypes.ChainConfig{
			Chain:  "eip155:1",
			VmType: uregistrytypes.VmType_EVM,
		}

		appConfig := testAppConfig("eip155:1", []string{server.URL})
		client, err := NewClient(config, nil, appConfig, logger)
		require.NoError(t, err)

		// Start the client
		ctx := context.Background()
		err = client.Start(ctx)
		require.NoError(t, err)
		defer client.Stop()

		// Get block number
		blockNum, err := client.GetLatestBlockNumber(ctx)
		assert.NoError(t, err)
		assert.NotNil(t, blockNum)
		assert.Equal(t, int64(0x1234), blockNum.Int64())
	})

	t.Run("Client not connected", func(t *testing.T) {
		config := &uregistrytypes.ChainConfig{
			Chain:  "eip155:1",
			VmType: uregistrytypes.VmType_EVM,
		}

		client, err := NewClient(config, nil, nil, logger)
		require.NoError(t, err)

		ctx := context.Background()
		blockNum, err := client.GetLatestBlockNumber(ctx)
		assert.Error(t, err)
		assert.Nil(t, blockNum)
		assert.Contains(t, err.Error(), "client not connected")
	})
}

// TestClientConcurrency tests concurrent operations
func TestClientConcurrency(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))

	// Create a mock HTTP server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Add small delay to simulate network latency
		time.Sleep(10 * time.Millisecond)
		response := `{"jsonrpc":"2.0","id":1,"result":"0x1"}`
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(response))
	}))
	defer server.Close()

	config := &uregistrytypes.ChainConfig{
		Chain:  "eip155:1",
		VmType: uregistrytypes.VmType_EVM,
	}

	appConfig := testAppConfig("eip155:1", []string{server.URL})
	client, err := NewClient(config, nil, appConfig, logger)
	require.NoError(t, err)

	ctx := context.Background()
	err = client.Start(ctx)
	require.NoError(t, err)
	defer client.Stop()

	// Run multiple health checks concurrently
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func() {
			healthy := client.IsHealthy()
			assert.True(t, healthy)
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}
}
