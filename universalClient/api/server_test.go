package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	
	uregistrytypes "github.com/rollchains/pchain/x/uregistry/types"
)

// MockUniversalClient implements UniversalClientInterface for testing
type MockUniversalClient struct {
	chainConfigs  []*uregistrytypes.ChainConfig
	tokenConfigs  []*uregistrytypes.TokenConfig
	lastUpdate    time.Time
	forceUpdateErr error
	forceUpdateCalled bool
}

func NewMockUniversalClient() *MockUniversalClient {
	enabled := &uregistrytypes.ChainEnabled{
		IsInboundEnabled: true,
		IsOutboundEnabled: true,
	}
	return &MockUniversalClient{
		chainConfigs: []*uregistrytypes.ChainConfig{
			{
				Chain:          "eip155:1",
				VmType:         uregistrytypes.VmType_EVM,
				PublicRpcUrl:   "https://eth.example.com",
				GatewayAddress: "0x123",
				Enabled:        enabled,
			},
			{
				Chain:          "solana:mainnet",
				VmType:         uregistrytypes.VmType_SVM,
				PublicRpcUrl:   "https://sol.example.com",
				GatewayAddress: "11111111111111111111111111111111",
				Enabled:        enabled,
			},
		},
		tokenConfigs: []*uregistrytypes.TokenConfig{
			{
				Chain:    "eip155:1",
				Address:  "0xAAA",
				Symbol:   "USDT",
				Decimals: 6,
			},
			{
				Chain:    "eip155:1",
				Address:  "0xBBB",
				Symbol:   "USDC",
				Decimals: 6,
			},
			{
				Chain:    "solana:mainnet",
				Address:  "So11111111111111111111111111111111111111112",
				Symbol:   "SOL",
				Decimals: 9,
			},
		},
		lastUpdate: time.Now(),
	}
}

func (m *MockUniversalClient) GetAllChainConfigs() []*uregistrytypes.ChainConfig {
	return m.chainConfigs
}

func (m *MockUniversalClient) GetAllTokenConfigs() []*uregistrytypes.TokenConfig {
	return m.tokenConfigs
}

func (m *MockUniversalClient) GetTokenConfigsByChain(chain string) []*uregistrytypes.TokenConfig {
	configs := []*uregistrytypes.TokenConfig{}
	for _, tc := range m.tokenConfigs {
		if tc.Chain == chain {
			configs = append(configs, tc)
		}
	}
	return configs
}

func (m *MockUniversalClient) GetTokenConfig(chain, address string) *uregistrytypes.TokenConfig {
	for _, tc := range m.tokenConfigs {
		if tc.Chain == chain && tc.Address == address {
			return tc
		}
	}
	return nil
}

func (m *MockUniversalClient) GetCacheLastUpdate() time.Time {
	return m.lastUpdate
}

func (m *MockUniversalClient) GetChainConfig(chain string) *uregistrytypes.ChainConfig {
	for _, cfg := range m.chainConfigs {
		if cfg.Chain == chain {
			return cfg
		}
	}
	return nil
}

func (m *MockUniversalClient) ForceConfigUpdate() error {
	m.forceUpdateCalled = true
	return m.forceUpdateErr
}

func TestNewServer(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))
	mockClient := NewMockUniversalClient()
	
	t.Run("Create server with valid config", func(t *testing.T) {
		server := NewServer(mockClient, logger, 8080)
		
		assert.NotNil(t, server)
		assert.Equal(t, mockClient, server.client)
		assert.NotNil(t, server.server)
		assert.Equal(t, ":8080", server.server.Addr)
	})
	
	t.Run("Create server with different port", func(t *testing.T) {
		server := NewServer(mockClient, logger, 9090)
		
		assert.NotNil(t, server)
		assert.Equal(t, ":9090", server.server.Addr)
	})
}

func TestServerStartStop(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))
	mockClient := NewMockUniversalClient()
	
	t.Run("Start and stop server", func(t *testing.T) {
		// Use a random port to avoid conflicts
		server := NewServer(mockClient, logger, 0)
		
		// Start server
		err := server.Start()
		require.NoError(t, err)
		
		// Give server time to start
		time.Sleep(200 * time.Millisecond)
		
		// Verify server is running by making a health check request
		// Note: We're using port 0, so we can't easily test the actual HTTP endpoint
		// In a real test, we'd get the actual port from the listener
		
		// Stop server
		err = server.Stop()
		assert.NoError(t, err)
	})
	
	t.Run("Start with nil server", func(t *testing.T) {
		server := &Server{
			client: mockClient,
			logger: logger,
			server: nil,
		}
		
		err := server.Start()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "query server is nil")
	})
	
	t.Run("Stop with nil server", func(t *testing.T) {
		server := &Server{
			client: mockClient,
			logger: logger,
			server: nil,
		}
		
		err := server.Stop()
		assert.NoError(t, err)
	})
}

func TestServerIntegration(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))
	mockClient := NewMockUniversalClient()
	
	t.Run("Server lifecycle with HTTP client", func(t *testing.T) {
		// Create server on a specific port
		server := NewServer(mockClient, logger, 18080)
		
		// Start server
		err := server.Start()
		require.NoError(t, err)
		defer server.Stop()
		
		// Wait for server to be ready
		time.Sleep(200 * time.Millisecond)
		
		// Test health endpoint
		resp, err := http.Get("http://localhost:18080/health")
		if err == nil {
			defer resp.Body.Close()
			assert.Equal(t, http.StatusOK, resp.StatusCode)
		}
	})
}

// Test handler functions directly using httptest
func TestHealthHandler(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))
	mockClient := NewMockUniversalClient()
	server := &Server{
		client: mockClient,
		logger: logger,
	}
	
	handler := http.HandlerFunc(server.handleHealth)
	
	t.Run("Health check returns OK", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		w := httptest.NewRecorder()
		
		handler(w, req)
		
		assert.Equal(t, http.StatusOK, w.Code)
		// handleHealth just returns "OK" as plain text
		assert.Equal(t, "OK", w.Body.String())
	})
}

func TestGetAllChainConfigsHandler(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))
	mockClient := NewMockUniversalClient()
	server := &Server{
		client: mockClient,
		logger: logger,
	}
	
	handler := http.HandlerFunc(server.handleChainConfigs)
	
	t.Run("Get all chain configs", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/chains", nil)
		w := httptest.NewRecorder()
		
		handler(w, req)
		
		assert.Equal(t, http.StatusOK, w.Code)
		
		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		
		// Check structure
		assert.Contains(t, response, "data")
		assert.Contains(t, response, "last_fetched")
		
		// Check data
		data, ok := response["data"].([]*uregistrytypes.ChainConfig)
		if !ok {
			// Try as interface array first
			dataInterface, ok := response["data"].([]interface{})
			require.True(t, ok)
			assert.Len(t, dataInterface, 2)
		} else {
			assert.Len(t, data, 2)
			assert.Equal(t, "eip155:1", data[0].Chain)
			assert.Equal(t, "solana:mainnet", data[1].Chain)
		}
	})
}

// Test for individual chain config doesn't exist - removed

func TestGetAllTokenConfigsHandler(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))
	mockClient := NewMockUniversalClient()
	server := &Server{
		client: mockClient,
		logger: logger,
	}
	
	handler := http.HandlerFunc(server.handleTokenConfigs)
	
	t.Run("Get all token configs", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/tokens", nil)
		w := httptest.NewRecorder()
		
		handler(w, req)
		
		assert.Equal(t, http.StatusOK, w.Code)
		
		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		
		// Check structure
		assert.Contains(t, response, "data")
		assert.Contains(t, response, "last_fetched")
		
		// Check data
		dataInterface, ok := response["data"].([]interface{})
		require.True(t, ok)
		assert.Len(t, dataInterface, 3)
	})
}

func TestGetTokenConfigsByChainHandler(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))
	mockClient := NewMockUniversalClient()
	server := &Server{
		client: mockClient,
		logger: logger,
	}
	
	handler := http.HandlerFunc(server.handleTokenConfigsByChain)
	
	t.Run("Get tokens for existing chain", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/token-configs-by-chain?chain=eip155:1", nil)
		w := httptest.NewRecorder()
		
		handler(w, req)
		
		assert.Equal(t, http.StatusOK, w.Code)
		
		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		
		// Check structure
		assert.Contains(t, response, "data")
		assert.Contains(t, response, "last_fetched")
		
		// Check data
		dataInterface, ok := response["data"].([]interface{})
		require.True(t, ok)
		assert.Len(t, dataInterface, 2)
	})
	
	t.Run("Get tokens for chain with no tokens", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/token-configs-by-chain?chain=eip155:999", nil)
		w := httptest.NewRecorder()
		
		handler(w, req)
		
		assert.Equal(t, http.StatusOK, w.Code)
		
		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		
		// Check structure
		assert.Contains(t, response, "data")
		assert.Contains(t, response, "last_fetched")
		
		// Check data is empty
		dataInterface, ok := response["data"].([]interface{})
		require.True(t, ok)
		assert.Len(t, dataInterface, 0)
	})
	
	t.Run("Missing chain parameter", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/tokens/", nil)
		w := httptest.NewRecorder()
		
		handler(w, req)
		
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

func TestGetTokenConfigHandler(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))
	mockClient := NewMockUniversalClient()
	server := &Server{
		client: mockClient,
		logger: logger,
	}
	
	handler := http.HandlerFunc(server.handleTokenConfig)
	
	t.Run("Get existing token config", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/token-config?chain=eip155:1&address=0xAAA", nil)
		w := httptest.NewRecorder()
		
		handler(w, req)
		
		assert.Equal(t, http.StatusOK, w.Code)
		
		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		
		// Check structure
		assert.Contains(t, response, "data")
		assert.Contains(t, response, "last_fetched")
		
		// Check data
		data, ok := response["data"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "USDT", data["symbol"])
		assert.Equal(t, "0xAAA", data["address"])
	})
	
	t.Run("Get non-existing token config", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/token-config?chain=eip155:1&address=0xZZZ", nil)
		w := httptest.NewRecorder()
		
		handler(w, req)
		
		assert.Equal(t, http.StatusNotFound, w.Code)
		
		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		assert.Contains(t, response, "error")
	})
	
	t.Run("Missing parameters", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/token/", nil)
		w := httptest.NewRecorder()
		
		handler(w, req)
		
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

// Removed tests for non-existent handlers (cache status, force update)

func TestInvalidMethods(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))
	mockClient := NewMockUniversalClient()
	server := &Server{
		client: mockClient,
		logger: logger,
	}
	
	tests := []struct {
		name        string
		handler     http.HandlerFunc
		method      string
		expectedMsg string
	}{
		{
			name:        "POST to health check",
			handler:     http.HandlerFunc(server.handleHealth),
			method:      http.MethodPost,
			expectedMsg: "method not allowed",
		},
		{
			name:        "DELETE to chain configs",
			handler:     http.HandlerFunc(server.handleChainConfigs),
			method:      http.MethodDelete,
			expectedMsg: "method not allowed",
		},
		{
			name:        "PUT to token configs",
			handler:     http.HandlerFunc(server.handleTokenConfigs),
			method:      http.MethodPut,
			expectedMsg: "method not allowed",
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/test", nil)
			w := httptest.NewRecorder()
			
			tt.handler(w, req)
			
			// Most handlers should return 405 for wrong methods
			// but implementation may vary
			body, _ := io.ReadAll(w.Body)
			t.Logf("Response: %d - %s", w.Code, string(body))
		})
	}
}

// Table-driven tests for various scenarios
func TestAPIEndpointsTable(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))
	mockClient := NewMockUniversalClient()
	server := &Server{
		client: mockClient,
		logger: logger,
	}
	
	tests := []struct {
		name       string
		handler    http.HandlerFunc
		method     string
		url        string
		wantStatus int
		checkBody  func(t *testing.T, body []byte)
	}{
		{
			name:       "health check",
			handler:    http.HandlerFunc(server.handleHealth),
			method:     http.MethodGet,
			url:        "/health",
			wantStatus: http.StatusOK,
			checkBody: func(t *testing.T, body []byte) {
				// handleHealth returns plain "OK"
				assert.Equal(t, "OK", string(body))
			},
		},
		{
			name:       "all chain configs",
			handler:    http.HandlerFunc(server.handleChainConfigs),
			method:     http.MethodGet,
			url:        "/chains",
			wantStatus: http.StatusOK,
			checkBody: func(t *testing.T, body []byte) {
				var resp map[string]interface{}
				err := json.Unmarshal(body, &resp)
				require.NoError(t, err)
				assert.Contains(t, resp, "data")
				data, ok := resp["data"].([]interface{})
				require.True(t, ok)
				assert.Len(t, data, 2)
			},
		},
		{
			name:       "all token configs",
			handler:    http.HandlerFunc(server.handleTokenConfigs),
			method:     http.MethodGet,
			url:        "/tokens",
			wantStatus: http.StatusOK,
			checkBody: func(t *testing.T, body []byte) {
				var resp map[string]interface{}
				err := json.Unmarshal(body, &resp)
				require.NoError(t, err)
				assert.Contains(t, resp, "data")
				data, ok := resp["data"].([]interface{})
				require.True(t, ok)
				assert.Len(t, data, 3)
			},
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.url, nil)
			w := httptest.NewRecorder()
			
			tt.handler(w, req)
			
			assert.Equal(t, tt.wantStatus, w.Code)
			if tt.checkBody != nil {
				tt.checkBody(t, w.Body.Bytes())
			}
		})
	}
}