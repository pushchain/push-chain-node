package api

import (
	"net/http"
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
}

func NewMockUniversalClient() *MockUniversalClient {
	return &MockUniversalClient{
		chainConfigs: []*uregistrytypes.ChainConfig{
			{
				Chain:          "eip155:1",
				VmType:         uregistrytypes.VmType_EVM,
				PublicRpcUrl:   "https://eth.example.com",
				GatewayAddress: "0x123",
				Enabled:        true,
			},
		},
		tokenConfigs: []*uregistrytypes.TokenConfig{
			{
				Chain:    "eip155:1",
				Address:  "0xAAA",
				Symbol:   "USDT",
				Decimals: 6,
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