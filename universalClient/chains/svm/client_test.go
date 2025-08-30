package svm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rollchains/pchain/universalClient/config"
	uregistrytypes "github.com/rollchains/pchain/x/uregistry/types"
)

// TestClientInitialization tests the creation of Solana client
func TestClientInitialization(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))

	t.Run("Valid config", func(t *testing.T) {
		config := &uregistrytypes.ChainConfig{
			Chain:          "solana:EtWTRABZaYq6iMfeYKouRu166VU2xqa1",
			VmType:         uregistrytypes.VmType_SVM,
			PublicRpcUrl:   "https://api.devnet.solana.com",
			GatewayAddress: "Sol123...",
			Enabled:        true,
		}

		client, err := NewClient(config, nil, nil, logger)
		require.NoError(t, err)
		assert.NotNil(t, client)
		assert.Equal(t, "EtWTRABZaYq6iMfeYKouRu166VU2xqa1", client.genesisHash)
		assert.Equal(t, "https://api.devnet.solana.com", client.rpcURL)
		assert.Equal(t, config, client.GetConfig())
		assert.Equal(t, "solana:EtWTRABZaYq6iMfeYKouRu166VU2xqa1", client.ChainID())
	})

	t.Run("Nil config", func(t *testing.T) {
		client, err := NewClient(nil, nil, nil, logger)
		assert.Error(t, err)
		assert.Nil(t, client)
		assert.Contains(t, err.Error(), "config is nil")
	})

	t.Run("Invalid VM type", func(t *testing.T) {
		config := &uregistrytypes.ChainConfig{
			Chain:  "solana:mainnet",
			VmType: uregistrytypes.VmType_EVM, // Wrong VM type
		}

		client, err := NewClient(config, nil, nil, logger)
		assert.Error(t, err)
		assert.Nil(t, client)
		assert.Contains(t, err.Error(), "invalid VM type for Solana client")
	})

	t.Run("Invalid chain ID format", func(t *testing.T) {
		config := &uregistrytypes.ChainConfig{
			Chain:  "invalid:format:extra",
			VmType: uregistrytypes.VmType_SVM,
		}

		client, err := NewClient(config, nil, nil, logger)
		assert.Error(t, err)
		assert.Nil(t, client)
		assert.Contains(t, err.Error(), "invalid CAIP-2 format")
	})

	t.Run("Wrong namespace", func(t *testing.T) {
		config := &uregistrytypes.ChainConfig{
			Chain:  "eip155:1",
			VmType: uregistrytypes.VmType_SVM,
		}

		client, err := NewClient(config, nil, nil, logger)
		assert.Error(t, err)
		assert.Nil(t, client)
		assert.Contains(t, err.Error(), "not a Solana chain")
	})
}

// TestParseSolanaChainID tests the CAIP-2 chain ID parsing
func TestParseSolanaChainID(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		expected  string
		expectErr bool
		errMsg    string
	}{
		{
			name:     "Valid mainnet",
			input:    "solana:5eykt4UsFv8P8NJdTREpY1vzqKqZKvdp",
			expected: "5eykt4UsFv8P8NJdTREpY1vzqKqZKvdp",
		},
		{
			name:     "Valid devnet",
			input:    "solana:EtWTRABZaYq6iMfeYKouRu166VU2xqa1",
			expected: "EtWTRABZaYq6iMfeYKouRu166VU2xqa1",
		},
		{
			name:      "Invalid format - missing colon",
			input:     "solanamainnet",
			expectErr: true,
			errMsg:    "invalid CAIP-2 format",
		},
		{
			name:      "Invalid format - too many parts",
			input:     "solana:mainnet:extra",
			expectErr: true,
			errMsg:    "invalid CAIP-2 format",
		},
		{
			name:      "Wrong namespace",
			input:     "eip155:1",
			expectErr: true,
			errMsg:    "not a Solana chain",
		},
		{
			name:      "Empty network ID",
			input:     "solana:",
			expectErr: true,
			errMsg:    "empty genesis hash",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseSolanaChainID(tt.input)

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
			// Mock getHealth response - Solana returns a string directly
			response := map[string]interface{}{
				"jsonrpc": "2.0",
				"result":  "ok",
				"id":      1,
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)
		}))
		defer server.Close()

		chainConfig := &uregistrytypes.ChainConfig{
			Chain:          "solana:devnet",
			VmType:         uregistrytypes.VmType_SVM,
			PublicRpcUrl:   server.URL,
			GatewayAddress: "Sol123...",
			Enabled:        true,
		}

		// Create appConfig with RPC URLs
		appConfig := &config.Config{
			ChainRPCURLs: map[string][]string{
				"solana:devnet": {server.URL},
			},
		}

		client, err := NewClient(chainConfig, nil, appConfig, logger)
		require.NoError(t, err)

		ctx := context.Background()
		err = client.Start(ctx)
		assert.NoError(t, err)
		assert.NotNil(t, client.rpcClient)

		// Test Stop
		err = client.Stop()
		assert.NoError(t, err)
		assert.Nil(t, client.rpcClient)
	})

	t.Run("Start with invalid URL", func(t *testing.T) {
		chainConfig := &uregistrytypes.ChainConfig{
			Chain:          "solana:devnet",
			VmType:         uregistrytypes.VmType_SVM,
			PublicRpcUrl:   "http://invalid.localhost:99999",
			GatewayAddress: "Sol123...",
		}

		// Create appConfig with invalid RPC URL
		appConfig := &config.Config{
			ChainRPCURLs: map[string][]string{
				"solana:devnet": {"http://invalid.localhost:99999"},
			},
		}

		client, err := NewClient(chainConfig, nil, appConfig, logger)
		require.NoError(t, err)

		ctx := context.Background()
		err = client.Start(ctx)
		assert.Error(t, err)
	})

	t.Run("Start with empty URL", func(t *testing.T) {
		config := &uregistrytypes.ChainConfig{
			Chain:  "solana:devnet",
			VmType: uregistrytypes.VmType_SVM,
			// Empty URL
			PublicRpcUrl: "",
		}

		// Don't provide any RPC URLs in appConfig
		client, err := NewClient(config, nil, nil, logger)
		require.NoError(t, err)

		ctx := context.Background()
		err = client.Start(ctx)
		assert.Error(t, err)
		// Should get error about no RPC URLs configured
		assert.Contains(t, err.Error(), "no RPC URLs configured")
	})
}

// TestClientIsHealthy tests the health check functionality
func TestClientIsHealthy(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))

	t.Run("Healthy client", func(t *testing.T) {
		// Create a mock HTTP server
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			response := map[string]interface{}{
				"jsonrpc": "2.0",
				"result":  "ok",
				"id":      1,
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)
		}))
		defer server.Close()

		chainConfig := &uregistrytypes.ChainConfig{
			Chain:        "solana:devnet",
			VmType:       uregistrytypes.VmType_SVM,
			PublicRpcUrl: server.URL,
		}

		// Create appConfig with RPC URL
		appConfig := &config.Config{
			ChainRPCURLs: map[string][]string{
				"solana:devnet": {server.URL},
			},
		}

		client, err := NewClient(chainConfig, nil, appConfig, logger)
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
			Chain:  "solana:devnet",
			VmType: uregistrytypes.VmType_SVM,
		}

		client, err := NewClient(config, nil, nil, logger)
		require.NoError(t, err)

		healthy := client.IsHealthy()
		assert.False(t, healthy)
	})

	t.Run("Not healthy - server error", func(t *testing.T) {
		// Create a mock HTTP server that returns errors
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		chainConfig := &uregistrytypes.ChainConfig{
			Chain:        "solana:devnet",
			VmType:       uregistrytypes.VmType_SVM,
			PublicRpcUrl: server.URL,
		}

		// Create appConfig with RPC URL pointing to error server
		appConfig := &config.Config{
			ChainRPCURLs: map[string][]string{
				"solana:devnet": {server.URL},
			},
		}

		client, err := NewClient(chainConfig, nil, appConfig, logger)
		require.NoError(t, err)

		// Start the client - should fail because getHealth returns error
		ctx := context.Background()
		err = client.Start(ctx)
		assert.Error(t, err)

		// Check health - should be false because client failed to start
		healthy := client.IsHealthy()
		assert.False(t, healthy)
	})
}

// TestClientGetMethods tests getter methods
func TestClientGetMethods(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))

	config := &uregistrytypes.ChainConfig{
		Chain:          "solana:EtWTRABZaYq6iMfeYKouRu166VU2xqa1",
		VmType:         uregistrytypes.VmType_SVM,
		PublicRpcUrl:   "https://api.devnet.solana.com",
		GatewayAddress: "Sol123...",
		Enabled:        true,
	}

	client, err := NewClient(config, nil, nil, logger)
	require.NoError(t, err)

	t.Run("GetGenesisHash", func(t *testing.T) {
		assert.Equal(t, "EtWTRABZaYq6iMfeYKouRu166VU2xqa1", client.GetGenesisHash())
	})

	t.Run("GetRPCURL", func(t *testing.T) {
		assert.Equal(t, "https://api.devnet.solana.com", client.GetRPCURL())
	})

	t.Run("ChainID", func(t *testing.T) {
		assert.Equal(t, "solana:EtWTRABZaYq6iMfeYKouRu166VU2xqa1", client.ChainID())
	})

	t.Run("GetConfig", func(t *testing.T) {
		assert.Equal(t, config, client.GetConfig())
	})
}

// TestClientGetSlot tests the GetSlot method
func TestClientGetSlot(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))

	t.Run("Success", func(t *testing.T) {
		// Create a mock HTTP server
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Read the request body to determine which method is being called
			var reqBody map[string]interface{}
			json.NewDecoder(r.Body).Decode(&reqBody)
			
			var response map[string]interface{}
			method, _ := reqBody["method"].(string)
			
			switch method {
			case "getHealth":
				response = map[string]interface{}{
					"jsonrpc": "2.0",
					"result":  "ok",
					"id":      reqBody["id"],
				}
			case "getSlot":
				response = map[string]interface{}{
					"jsonrpc": "2.0",
					"result":  uint64(123456),
					"id":      reqBody["id"],
				}
			default:
				response = map[string]interface{}{
					"jsonrpc": "2.0",
					"error": map[string]interface{}{
						"code":    -32601,
						"message": "Method not found",
					},
					"id": reqBody["id"],
				}
			}
			
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)
		}))
		defer server.Close()

		chainConfig := &uregistrytypes.ChainConfig{
			Chain:        "solana:devnet",
			VmType:       uregistrytypes.VmType_SVM,
			PublicRpcUrl: server.URL,
		}

		// Create appConfig with RPC URL
		appConfig := &config.Config{
			ChainRPCURLs: map[string][]string{
				"solana:devnet": {server.URL},
			},
		}

		client, err := NewClient(chainConfig, nil, appConfig, logger)
		require.NoError(t, err)

		// Start the client
		ctx := context.Background()
		err = client.Start(ctx)
		require.NoError(t, err)
		defer client.Stop()

		// Get slot
		slot, err := client.GetSlot(ctx)
		assert.NoError(t, err)
		assert.Equal(t, uint64(123456), slot)
	})

	t.Run("Client not connected", func(t *testing.T) {
		config := &uregistrytypes.ChainConfig{
			Chain:  "solana:devnet",
			VmType: uregistrytypes.VmType_SVM,
		}

		client, err := NewClient(config, nil, nil, logger)
		require.NoError(t, err)

		ctx := context.Background()
		slot, err := client.GetSlot(ctx)
		assert.Error(t, err)
		assert.Equal(t, uint64(0), slot)
		assert.Contains(t, err.Error(), "client not connected")
	})

	t.Run("RPC error", func(t *testing.T) {
		// Create a mock HTTP server that returns proper responses for health but error for slot
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Read the request body to determine which method is being called
			var reqBody map[string]interface{}
			json.NewDecoder(r.Body).Decode(&reqBody)
			
			var response map[string]interface{}
			method, _ := reqBody["method"].(string)
			
			if method == "getHealth" {
				response = map[string]interface{}{
					"jsonrpc": "2.0",
					"result":  "ok",
					"id":      reqBody["id"],
				}
			} else {
				response = map[string]interface{}{
					"jsonrpc": "2.0",
					"error": map[string]interface{}{
						"code":    -32601,
						"message": "Method not found",
					},
					"id": reqBody["id"],
				}
			}
			
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)
		}))
		defer server.Close()

		chainConfig := &uregistrytypes.ChainConfig{
			Chain:        "solana:devnet",
			VmType:       uregistrytypes.VmType_SVM,
			PublicRpcUrl: server.URL,
		}

		// Create appConfig with RPC URL
		appConfig := &config.Config{
			ChainRPCURLs: map[string][]string{
				"solana:devnet": {server.URL},
			},
		}

		client, err := NewClient(chainConfig, nil, appConfig, logger)
		require.NoError(t, err)

		// Start the client
		ctx := context.Background()
		err = client.Start(ctx)
		require.NoError(t, err)
		defer client.Stop()

		// Get slot - should return error
		slot, err := client.GetSlot(ctx)
		assert.Error(t, err)
		assert.Equal(t, uint64(0), slot)
	})
}

// TestClientConcurrency tests concurrent operations
func TestClientConcurrency(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))

	// Create a mock HTTP server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Add small delay to simulate network latency
		time.Sleep(10 * time.Millisecond)
		response := map[string]interface{}{
			"jsonrpc": "2.0",
			"result":  "ok",
			"id":      1,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	chainConfig := &uregistrytypes.ChainConfig{
		Chain:        "solana:devnet",
		VmType:       uregistrytypes.VmType_SVM,
		PublicRpcUrl: server.URL,
	}

	// Create appConfig with RPC URL
	appConfig := &config.Config{
		ChainRPCURLs: map[string][]string{
			"solana:devnet": {server.URL},
		},
	}

	client, err := NewClient(chainConfig, nil, appConfig, logger)
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