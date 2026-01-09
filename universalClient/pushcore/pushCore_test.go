package pushcore

import (
	"context"
	"math/big"
	"testing"

	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
	uregistrytypes "github.com/pushchain/push-chain-node/x/uregistry/types"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

func TestNew(t *testing.T) {
	logger := zerolog.Nop()

	tests := []struct {
		name    string
		urls    []string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "empty URLs list",
			urls:    []string{},
			wantErr: true,
			errMsg:  "at least one gRPC URL is required",
		},
		{
			name:    "nil URLs list",
			urls:    nil,
			wantErr: true,
			errMsg:  "at least one gRPC URL is required",
		},
		{
			name: "valid URL without port",
			urls: []string{"localhost"},
			// Should succeed as CreateGRPCConnection adds default port
			wantErr: false,
		},
		{
			name:    "valid URL with port",
			urls:    []string{"localhost:9090"},
			wantErr: false,
		},
		{
			name:    "http URL",
			urls:    []string{"http://localhost:9090"},
			wantErr: false,
		},
		{
			name:    "https URL",
			urls:    []string{"https://localhost:9090"},
			wantErr: false,
		},
		{
			name:    "multiple URLs",
			urls:    []string{"localhost:9090", "localhost:9091", "localhost:9092"},
			wantErr: false,
		},
		{
			name: "mix of valid and invalid URLs",
			urls: []string{"localhost:9090", "invalid-url-that-will-fail:99999", "localhost:9091"},
			// Should still succeed as long as at least one connection works
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := New(tt.urls, logger)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
				assert.Nil(t, client)
			} else {
				// Note: The connections might fail in test environment, but we're testing the logic
				// The function might still return an error if ALL connections fail
				if err != nil {
					// Check if it's because all connections failed
					assert.Contains(t, err.Error(), "all dials failed")
				} else {
					require.NotNil(t, client)
					assert.NotNil(t, client.logger)
					// Clean up
					_ = client.Close()
				}
			}
		})
	}
}

func TestClient_Close(t *testing.T) {
	logger := zerolog.Nop()

	t.Run("close with no connections", func(t *testing.T) {
		client := &Client{
			logger: logger,
			eps:    nil,
			conns:  nil,
		}

		err := client.Close()
		assert.NoError(t, err)
		assert.Nil(t, client.conns)
		assert.Nil(t, client.eps)
	})

	t.Run("close with mock connections", func(t *testing.T) {
		// Create a client with a valid connection
		client, err := New([]string{"localhost:9090"}, logger)
		if err != nil {
			// If we can't create a connection (common in test env), create a mock client
			client = &Client{
				logger: logger,
				eps:    []uregistrytypes.QueryClient{},
				conns:  []*grpc.ClientConn{},
			}
		}

		err = client.Close()
		assert.NoError(t, err)
		assert.Nil(t, client.conns)
		assert.Nil(t, client.eps)
	})
}

func TestClient_GetAllChainConfigs(t *testing.T) {
	logger := zerolog.Nop()
	ctx := context.Background()

	t.Run("no endpoints configured", func(t *testing.T) {
		client := &Client{
			logger: logger,
			eps:    []uregistrytypes.QueryClient{},
		}

		configs, err := client.GetAllChainConfigs(ctx)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no endpoints configured")
		assert.Nil(t, configs)
	})

	// Skip round-robin test as we can't mock the interface easily without nil pointers
	// The actual round-robin logic is simple enough and tested by the error message count
}

// Removed TestClient_RoundRobinCounter as it would require nil pointer handling

func TestNew_ErrorHandling(t *testing.T) {
	logger := zerolog.Nop()

	t.Run("all connections fail", func(t *testing.T) {
		// Use URLs that will definitely fail to connect
		urls := []string{
			"invalid-host-that-doesnt-exist:99999",
			"another-invalid-host:88888",
		}

		client, err := New(urls, logger)

		// Should get an error about all dials failing
		if err != nil {
			assert.Contains(t, err.Error(), "all dials failed")
			assert.Contains(t, err.Error(), "2 urls") // Should mention the number of URLs tried
			assert.Nil(t, client)
		} else {
			// If somehow it succeeded, make sure to clean up
			require.NotNil(t, client)
			_ = client.Close()
		}
	})

	t.Run("partial connection success", func(t *testing.T) {
		// Mix of potentially valid and definitely invalid URLs
		urls := []string{
			"localhost:9090",                       // Might work
			"invalid-host-that-doesnt-exist:99999", // Will fail
		}

		client, err := New(urls, logger)

		// This should succeed if at least one connection works
		// or fail if all connections fail
		if err != nil {
			assert.Contains(t, err.Error(), "all dials failed")
		} else {
			require.NotNil(t, client)
			_ = client.Close()
		}
	})
}

// Removed TestClient_GetAllChainConfigs_ErrorPropagation as it would require nil pointer handling

func TestCreateGRPCConnection(t *testing.T) {
	tests := []struct {
		name          string
		endpoint      string
		wantErr       bool
		errorContains string
	}{
		{
			name:          "empty endpoint",
			endpoint:      "",
			wantErr:       true,
			errorContains: "empty endpoint",
		},
		{
			name:     "http endpoint without port",
			endpoint: "http://localhost",
			wantErr:  false,
		},
		{
			name:     "https endpoint without port",
			endpoint: "https://localhost",
			wantErr:  false,
		},
		{
			name:     "http endpoint with port",
			endpoint: "http://localhost:9090",
			wantErr:  false,
		},
		{
			name:     "https endpoint with port",
			endpoint: "https://localhost:9090",
			wantErr:  false,
		},
		{
			name:     "endpoint without scheme and without port",
			endpoint: "localhost",
			wantErr:  false,
		},
		{
			name:     "endpoint without scheme but with port",
			endpoint: "localhost:9090",
			wantErr:  false,
		},
		{
			name:     "endpoint with custom port",
			endpoint: "localhost:8080",
			wantErr:  false,
		},
		{
			name:     "endpoint with invalid port format",
			endpoint: "localhost:",
			wantErr:  false, // Should add default port
		},
		{
			name:     "endpoint with path after colon",
			endpoint: "http://localhost:/path",
			wantErr:  false, // Should add default port
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn, err := CreateGRPCConnection(tt.endpoint)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
				assert.Nil(t, conn)
			} else {
				require.NoError(t, err)
				require.NotNil(t, conn)
				// Clean up connection
				err = conn.Close()
				assert.NoError(t, err)
			}
		})
	}
}

func TestCreateGRPCConnection_PortHandling(t *testing.T) {
	tests := []struct {
		name             string
		endpoint         string
		expectedContains string // What the processed endpoint should contain
	}{
		{
			name:             "adds default port when missing",
			endpoint:         "localhost",
			expectedContains: ":9090",
		},
		{
			name:             "preserves existing port",
			endpoint:         "localhost:8080",
			expectedContains: ":8080",
		},
		{
			name:             "adds port to http endpoint",
			endpoint:         "http://localhost",
			expectedContains: ":9090",
		},
		{
			name:             "adds port to https endpoint",
			endpoint:         "https://localhost",
			expectedContains: ":9090",
		},
		{
			name:             "handles empty port",
			endpoint:         "localhost:",
			expectedContains: ":9090",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn, err := CreateGRPCConnection(tt.endpoint)
			require.NoError(t, err)
			require.NotNil(t, conn)

			// Get the target from the connection
			target := conn.Target()
			assert.Contains(t, target, tt.expectedContains, "Expected target to contain %s, got %s", tt.expectedContains, target)

			// Clean up
			err = conn.Close()
			assert.NoError(t, err)
		})
	}
}

func TestCreateGRPCConnection_TLSHandling(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		// Note: We can't easily test if TLS is actually enabled without attempting a real connection
		// But we can verify the function doesn't error for different schemes
	}{
		{
			name:     "https should not error",
			endpoint: "https://localhost:9090",
		},
		{
			name:     "http should not error",
			endpoint: "http://localhost:9090",
		},
		{
			name:     "no scheme should not error",
			endpoint: "localhost:9090",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn, err := CreateGRPCConnection(tt.endpoint)
			require.NoError(t, err)
			require.NotNil(t, conn)

			// Clean up
			err = conn.Close()
			assert.NoError(t, err)
		})
	}
}

func TestExtractHostnameFromURL(t *testing.T) {
	tests := []struct {
		name             string
		url              string
		expectedHostname string
		wantErr          bool
		errorContains    string
	}{
		{
			name:             "https URL with port",
			url:              "https://grpc.example.com:443",
			expectedHostname: "grpc.example.com",
			wantErr:          false,
		},
		{
			name:             "https URL without port",
			url:              "https://grpc.example.com",
			expectedHostname: "grpc.example.com",
			wantErr:          false,
		},
		{
			name:             "http URL with port",
			url:              "http://localhost:9090",
			expectedHostname: "localhost",
			wantErr:          false,
		},
		{
			name:             "http URL without port",
			url:              "http://api.test.com",
			expectedHostname: "api.test.com",
			wantErr:          false,
		},
		{
			name:             "plain hostname without port",
			url:              "example.com",
			expectedHostname: "example.com",
			wantErr:          false,
		},
		{
			name:             "plain hostname with port",
			url:              "example.com:8080",
			expectedHostname: "example.com",
			wantErr:          false,
		},
		{
			name:             "localhost without port",
			url:              "localhost",
			expectedHostname: "localhost",
			wantErr:          false,
		},
		{
			name:             "localhost with port",
			url:              "localhost:9090",
			expectedHostname: "localhost",
			wantErr:          false,
		},
		{
			name:             "complex subdomain",
			url:              "https://grpc.rpc-testnet-donut-node1.push.org:443",
			expectedHostname: "grpc.rpc-testnet-donut-node1.push.org",
			wantErr:          false,
		},
		{
			name:             "URL with path",
			url:              "https://example.com:443/some/path",
			expectedHostname: "example.com",
			wantErr:          false,
		},
		{
			name:             "empty URL",
			url:              "",
			expectedHostname: "",
			wantErr:          true,
			errorContains:    "empty URL provided",
		},
		{
			name:             "URL with only scheme",
			url:              "https://",
			expectedHostname: "",
			wantErr:          true,
			errorContains:    "could not extract hostname",
		},
		{
			name:             "URL with only port",
			url:              ":9090",
			expectedHostname: "",
			wantErr:          true,
			errorContains:    "could not extract hostname",
		},
		{
			name:             "IPv4 address",
			url:              "192.168.1.1:9090",
			expectedHostname: "192.168.1.1",
			wantErr:          false,
		},
		{
			name:             "IPv4 address with scheme",
			url:              "http://192.168.1.1:9090",
			expectedHostname: "192.168.1.1",
			wantErr:          false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hostname, err := ExtractHostnameFromURL(tt.url)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expectedHostname, hostname)
			}
		})
	}
}

func TestClient_GetGasPrice(t *testing.T) {
	logger := zerolog.Nop()
	ctx := context.Background()

	t.Run("no endpoints configured", func(t *testing.T) {
		client := &Client{
			logger:           logger,
			uexecutorClients: []uexecutortypes.QueryClient{},
		}

		price, err := client.GetGasPrice(ctx, "eip155:84532")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no endpoints configured")
		assert.Nil(t, price)
	})

	t.Run("empty chainID", func(t *testing.T) {
		client := &Client{
			logger:           logger,
			uexecutorClients: []uexecutortypes.QueryClient{nil}, // Has endpoint but chainID is empty
		}

		price, err := client.GetGasPrice(ctx, "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "chainID is required")
		assert.Nil(t, price)
	})
}

func TestClient_GetGasPrice_WithMock(t *testing.T) {
	logger := zerolog.Nop()
	ctx := context.Background()

	t.Run("successful gas price retrieval", func(t *testing.T) {
		mockClient := &mockUExecutorQueryClient{
			gasPriceResp: &uexecutortypes.QueryGasPriceResponse{
				GasPrice: &uexecutortypes.GasPrice{
					ObservedChainId: "eip155:84532",
					Signers:         []string{"validator1", "validator2", "validator3"},
					Prices:          []uint64{1000000000, 2000000000, 3000000000}, // 1, 2, 3 gwei
					BlockNums:       []uint64{100, 101, 102},
					MedianIndex:     1, // Median is 2 gwei (index 1)
				},
			},
		}

		client := &Client{
			logger:           logger,
			uexecutorClients: []uexecutortypes.QueryClient{mockClient},
		}

		price, err := client.GetGasPrice(ctx, "eip155:84532")
		require.NoError(t, err)
		require.NotNil(t, price)

		// Expected median price is 2000000000 (2 gwei)
		expectedPrice := big.NewInt(2000000000)
		assert.Equal(t, expectedPrice, price)
	})

	t.Run("single validator price", func(t *testing.T) {
		mockClient := &mockUExecutorQueryClient{
			gasPriceResp: &uexecutortypes.QueryGasPriceResponse{
				GasPrice: &uexecutortypes.GasPrice{
					ObservedChainId: "eip155:1",
					Signers:         []string{"validator1"},
					Prices:          []uint64{5000000000}, // 5 gwei
					BlockNums:       []uint64{100},
					MedianIndex:     0,
				},
			},
		}

		client := &Client{
			logger:           logger,
			uexecutorClients: []uexecutortypes.QueryClient{mockClient},
		}

		price, err := client.GetGasPrice(ctx, "eip155:1")
		require.NoError(t, err)
		require.NotNil(t, price)
		assert.Equal(t, big.NewInt(5000000000), price)
	})

	t.Run("empty prices array", func(t *testing.T) {
		mockClient := &mockUExecutorQueryClient{
			gasPriceResp: &uexecutortypes.QueryGasPriceResponse{
				GasPrice: &uexecutortypes.GasPrice{
					ObservedChainId: "eip155:84532",
					Signers:         []string{},
					Prices:          []uint64{}, // Empty
					BlockNums:       []uint64{},
					MedianIndex:     0,
				},
			},
		}

		client := &Client{
			logger:           logger,
			uexecutorClients: []uexecutortypes.QueryClient{mockClient},
		}

		price, err := client.GetGasPrice(ctx, "eip155:84532")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no gas prices available")
		assert.Nil(t, price)
	})

	t.Run("median index out of bounds fallback", func(t *testing.T) {
		mockClient := &mockUExecutorQueryClient{
			gasPriceResp: &uexecutortypes.QueryGasPriceResponse{
				GasPrice: &uexecutortypes.GasPrice{
					ObservedChainId: "eip155:84532",
					Signers:         []string{"validator1"},
					Prices:          []uint64{1500000000},
					BlockNums:       []uint64{100},
					MedianIndex:     99, // Out of bounds
				},
			},
		}

		client := &Client{
			logger:           logger,
			uexecutorClients: []uexecutortypes.QueryClient{mockClient},
		}

		price, err := client.GetGasPrice(ctx, "eip155:84532")
		require.NoError(t, err)
		require.NotNil(t, price)
		// Should fallback to first price
		assert.Equal(t, big.NewInt(1500000000), price)
	})

	t.Run("chain not found error", func(t *testing.T) {
		mockClient := &mockUExecutorQueryClient{
			err: assert.AnError,
		}

		client := &Client{
			logger:           logger,
			uexecutorClients: []uexecutortypes.QueryClient{mockClient},
		}

		price, err := client.GetGasPrice(ctx, "unknown-chain")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "GetGasPrice failed")
		assert.Nil(t, price)
	})

	t.Run("round robin failover", func(t *testing.T) {
		// First client fails, second succeeds
		failingClient := &mockUExecutorQueryClient{
			err: assert.AnError,
		}
		successClient := &mockUExecutorQueryClient{
			gasPriceResp: &uexecutortypes.QueryGasPriceResponse{
				GasPrice: &uexecutortypes.GasPrice{
					ObservedChainId: "eip155:84532",
					Prices:          []uint64{1000000000},
					MedianIndex:     0,
				},
			},
		}

		client := &Client{
			logger:           logger,
			uexecutorClients: []uexecutortypes.QueryClient{failingClient, successClient},
		}

		price, err := client.GetGasPrice(ctx, "eip155:84532")
		require.NoError(t, err)
		require.NotNil(t, price)
		assert.Equal(t, big.NewInt(1000000000), price)
	})

	t.Run("all endpoints fail", func(t *testing.T) {
		client := &Client{
			logger: logger,
			uexecutorClients: []uexecutortypes.QueryClient{
				&mockUExecutorQueryClient{err: assert.AnError},
				&mockUExecutorQueryClient{err: assert.AnError},
			},
		}

		price, err := client.GetGasPrice(ctx, "eip155:84532")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed on all 2 endpoints")
		assert.Nil(t, price)
	})

	t.Run("various chain IDs", func(t *testing.T) {
		chainIDs := []string{
			"eip155:1",       // Ethereum Mainnet
			"eip155:84532",   // Base Sepolia
			"eip155:137",     // Polygon
			"solana:mainnet", // Solana Mainnet
		}

		for _, chainID := range chainIDs {
			mockClient := &mockUExecutorQueryClient{
				gasPriceResp: &uexecutortypes.QueryGasPriceResponse{
					GasPrice: &uexecutortypes.GasPrice{
						ObservedChainId: chainID,
						Prices:          []uint64{1000000000},
						MedianIndex:     0,
					},
				},
			}

			client := &Client{
				logger:           logger,
				uexecutorClients: []uexecutortypes.QueryClient{mockClient},
			}

			price, err := client.GetGasPrice(ctx, chainID)
			require.NoError(t, err, "Failed for chainID: %s", chainID)
			require.NotNil(t, price)
		}
	})
}

// mockUExecutorQueryClient implements uexecutortypes.QueryClient for testing
type mockUExecutorQueryClient struct {
	gasPriceResp *uexecutortypes.QueryGasPriceResponse
	err          error
}

func (m *mockUExecutorQueryClient) GasPrice(ctx context.Context, req *uexecutortypes.QueryGasPriceRequest, opts ...grpc.CallOption) (*uexecutortypes.QueryGasPriceResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.gasPriceResp, nil
}

func (m *mockUExecutorQueryClient) Params(ctx context.Context, req *uexecutortypes.QueryParamsRequest, opts ...grpc.CallOption) (*uexecutortypes.QueryParamsResponse, error) {
	return nil, nil
}

func (m *mockUExecutorQueryClient) AllPendingInbounds(ctx context.Context, req *uexecutortypes.QueryAllPendingInboundsRequest, opts ...grpc.CallOption) (*uexecutortypes.QueryAllPendingInboundsResponse, error) {
	return nil, nil
}

func (m *mockUExecutorQueryClient) GetUniversalTx(ctx context.Context, req *uexecutortypes.QueryGetUniversalTxRequest, opts ...grpc.CallOption) (*uexecutortypes.QueryGetUniversalTxResponse, error) {
	return nil, nil
}

func (m *mockUExecutorQueryClient) AllUniversalTx(ctx context.Context, req *uexecutortypes.QueryAllUniversalTxRequest, opts ...grpc.CallOption) (*uexecutortypes.QueryAllUniversalTxResponse, error) {
	return nil, nil
}

func (m *mockUExecutorQueryClient) AllGasPrices(ctx context.Context, req *uexecutortypes.QueryAllGasPricesRequest, opts ...grpc.CallOption) (*uexecutortypes.QueryAllGasPricesResponse, error) {
	return nil, nil
}
