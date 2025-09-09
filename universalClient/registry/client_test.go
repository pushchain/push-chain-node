package registry

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	uregistrytypes "github.com/pushchain/push-chain-node/x/uregistry/types"
)

// TestRegistryClientMethods tests the registry client methods with mocks
func TestRegistryClientMethods(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockClient := NewMockQueryClient(ctrl)
	logger := zerolog.New(zerolog.NewTestWriter(t))

	// Create a mock connection info
	client := &RegistryClient{
		connections: []*connectionInfo{
			{
				url:         "localhost:9090",
				queryClient: mockClient,
				healthy:     true,
				lastCheck:   time.Now(),
			},
		},
		currentIdx:   0,
		logger:       logger,
		maxRetries:   3,
		retryBackoff: time.Millisecond, // Short for tests
	}

	ctx := context.Background()

	t.Run("GetChainConfig_Success", func(t *testing.T) {
		expectedConfig := &uregistrytypes.ChainConfig{
			Chain:          "eip155:11155111",
			VmType:         uregistrytypes.VmType_EVM,
			GatewayAddress: "0x123...",
			Enabled:        &uregistrytypes.ChainEnabled{IsInboundEnabled: true, IsOutboundEnabled: true},
		}

		mockClient.EXPECT().
			ChainConfig(gomock.Any(), &uregistrytypes.QueryChainConfigRequest{Chain: "eip155:11155111"}).
			Return(&uregistrytypes.QueryChainConfigResponse{Config: expectedConfig}, nil)

		config, err := client.GetChainConfig(ctx, "eip155:11155111")
		require.NoError(t, err)
		assert.Equal(t, expectedConfig, config)
	})

	t.Run("GetChainConfig_NotFound", func(t *testing.T) {
		mockClient.EXPECT().
			ChainConfig(gomock.Any(), &uregistrytypes.QueryChainConfigRequest{Chain: "invalid:chain"}).
			Return(nil, status.Error(codes.NotFound, "chain not found")).
			Times(4) // Initial + 3 retries

		_, err := client.GetChainConfig(ctx, "invalid:chain")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed after 4 attempts")
	})

	t.Run("GetAllChainConfigs_Success", func(t *testing.T) {
		expectedConfigs := []*uregistrytypes.ChainConfig{
			{
				Chain:   "eip155:11155111",
				VmType:  uregistrytypes.VmType_EVM,
				Enabled: &uregistrytypes.ChainEnabled{IsInboundEnabled: true, IsOutboundEnabled: true},
			},
			{
				Chain:   "solana:EtWTRABZaYq6iMfeYKouRu166VU2xqa1",
				VmType:  uregistrytypes.VmType_SVM,
				Enabled: &uregistrytypes.ChainEnabled{IsInboundEnabled: true, IsOutboundEnabled: true},
			},
		}

		mockClient.EXPECT().
			AllChainConfigs(gomock.Any(), &uregistrytypes.QueryAllChainConfigsRequest{}).
			Return(&uregistrytypes.QueryAllChainConfigsResponse{Configs: expectedConfigs}, nil)

		configs, err := client.GetAllChainConfigs(ctx)
		require.NoError(t, err)
		assert.Len(t, configs, 2)
		assert.Equal(t, expectedConfigs, configs)
	})

	t.Run("GetTokenConfig_Success", func(t *testing.T) {
		expectedToken := &uregistrytypes.TokenConfig{
			Chain:   "eip155:11155111",
			Address: "0xA0b86991c6218b36c1d19d4a2e9eb0ce3606eb48",
			Name:    "USD Coin",
			Symbol:  "USDC",
		}

		mockClient.EXPECT().
			TokenConfig(gomock.Any(), &uregistrytypes.QueryTokenConfigRequest{
				Chain:   "eip155:11155111",
				Address: "0xA0b86991c6218b36c1d19d4a2e9eb0ce3606eb48",
			}).
			Return(&uregistrytypes.QueryTokenConfigResponse{Config: expectedToken}, nil)

		token, err := client.GetTokenConfig(ctx, "eip155:11155111", "0xA0b86991c6218b36c1d19d4a2e9eb0ce3606eb48")
		require.NoError(t, err)
		assert.Equal(t, expectedToken, token)
	})

	t.Run("GetTokenConfigsByChain_Success", func(t *testing.T) {
		expectedTokens := []*uregistrytypes.TokenConfig{
			{
				Chain:   "eip155:11155111",
				Address: "0xAAA...",
				Name:    "Token A",
				Symbol:  "TKA",
			},
			{
				Chain:   "eip155:11155111",
				Address: "0xBBB...",
				Name:    "Token B",
				Symbol:  "TKB",
			},
		}

		mockClient.EXPECT().
			TokenConfigsByChain(gomock.Any(), &uregistrytypes.QueryTokenConfigsByChainRequest{
				Chain: "eip155:11155111",
			}).
			Return(&uregistrytypes.QueryTokenConfigsByChainResponse{Configs: expectedTokens}, nil)

		tokens, err := client.GetTokenConfigsByChain(ctx, "eip155:11155111")
		require.NoError(t, err)
		assert.Len(t, tokens, 2)
		assert.Equal(t, expectedTokens, tokens)
	})

	t.Run("GetAllTokenConfigs_Success", func(t *testing.T) {
		expectedTokens := []*uregistrytypes.TokenConfig{
			{
				Chain:   "eip155:11155111",
				Address: "0xAAA...",
				Name:    "Token A",
				Symbol:  "TKA",
			},
			{
				Chain:   "solana:EtWTRABZaYq6iMfeYKouRu166VU2xqa1",
				Address: "SolToken...",
				Name:    "Sol Token",
				Symbol:  "SOLT",
			},
		}

		mockClient.EXPECT().
			AllTokenConfigs(gomock.Any(), &uregistrytypes.QueryAllTokenConfigsRequest{}).
			Return(&uregistrytypes.QueryAllTokenConfigsResponse{Configs: expectedTokens}, nil)

		tokens, err := client.GetAllTokenConfigs(ctx)
		require.NoError(t, err)
		assert.Len(t, tokens, 2)
		assert.Equal(t, expectedTokens, tokens)
	})

	t.Run("RetryLogic_TransientError", func(t *testing.T) {
		// First two calls fail, third succeeds
		gomock.InOrder(
			mockClient.EXPECT().
				AllChainConfigs(gomock.Any(), gomock.Any()).
				Return(nil, errors.New("temporary error")),
			mockClient.EXPECT().
				AllChainConfigs(gomock.Any(), gomock.Any()).
				Return(nil, errors.New("temporary error")),
			mockClient.EXPECT().
				AllChainConfigs(gomock.Any(), gomock.Any()).
				Return(&uregistrytypes.QueryAllChainConfigsResponse{Configs: []*uregistrytypes.ChainConfig{}}, nil),
		)

		configs, err := client.GetAllChainConfigs(ctx)
		require.NoError(t, err)
		assert.NotNil(t, configs)
	})

	t.Run("RetryLogic_ContextCancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		// First call fails
		mockClient.EXPECT().
			AllChainConfigs(gomock.Any(), gomock.Any()).
			Return(nil, errors.New("temporary error")).
			AnyTimes() // May be called once or twice depending on timing

		// Cancel context before retry
		go func() {
			time.Sleep(2 * time.Millisecond)
			cancel()
		}()

		_, err := client.GetAllChainConfigs(ctx)
		assert.Error(t, err)
		// Either context.Canceled or wrapped error containing it
		assert.True(t, errors.Is(err, context.Canceled) || strings.Contains(err.Error(), "context canceled"))
	})
}

// TestRegistryClientRetryBackoff tests the exponential backoff logic
func TestRegistryClientRetryBackoff(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockClient := NewMockQueryClient(ctrl)
	logger := zerolog.New(zerolog.NewTestWriter(t))

	client := &RegistryClient{
		connections: []*connectionInfo{
			{
				url:         "localhost:9090",
				queryClient: mockClient,
				healthy:     true,
				lastCheck:   time.Now(),
			},
		},
		currentIdx:   0,
		logger:       logger,
		maxRetries:   3,
		retryBackoff: 10 * time.Millisecond, // Start with 10ms
	}

	ctx := context.Background()

	// Mock to always fail
	mockClient.EXPECT().
		AllChainConfigs(gomock.Any(), gomock.Any()).
		Return(nil, errors.New("persistent error")).
		Times(4) // Initial + 3 retries

	start := time.Now()
	_, err := client.GetAllChainConfigs(ctx)
	duration := time.Since(start)

	assert.Error(t, err)
	// With exponential backoff: 10ms + 20ms + 40ms = 70ms minimum
	assert.Greater(t, duration, 70*time.Millisecond)
	assert.Less(t, duration, 200*time.Millisecond) // Should not take too long
}

// TestRegistryClientMultiURLFailover tests failover between multiple URLs
func TestRegistryClientMultiURLFailover(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockClient1 := NewMockQueryClient(ctrl)
	mockClient2 := NewMockQueryClient(ctrl)
	logger := zerolog.New(zerolog.NewTestWriter(t))

	client := &RegistryClient{
		connections: []*connectionInfo{
			{
				url:         "localhost:9090",
				queryClient: mockClient1,
				healthy:     true,
				lastCheck:   time.Now(),
				conn:        nil, // Mock connection, no actual gRPC conn
			},
			{
				url:         "localhost:9091",
				queryClient: mockClient2,
				healthy:     true,
				lastCheck:   time.Now(),
				conn:        nil, // Mock connection, no actual gRPC conn
			},
		},
		currentIdx:          0,
		logger:              logger,
		maxRetries:          2,
		retryBackoff:        time.Millisecond,
		unhealthyCooldown:   10 * time.Second,
		healthCheckInterval: 30 * time.Second,
	}

	ctx := context.Background()

	t.Run("Failover_On_Connection_Error", func(t *testing.T) {
		// First connection fails with connection error
		mockClient1.EXPECT().
			AllChainConfigs(gomock.Any(), gomock.Any()).
			Return(nil, errors.New("connection refused")).
			Times(1)

		// Second connection succeeds
		expectedConfigs := []*uregistrytypes.ChainConfig{
			{Chain: "eip155:11155111", VmType: uregistrytypes.VmType_EVM},
		}
		mockClient2.EXPECT().
			AllChainConfigs(gomock.Any(), gomock.Any()).
			Return(&uregistrytypes.QueryAllChainConfigsResponse{Configs: expectedConfigs}, nil).
			Times(1)

		configs, err := client.GetAllChainConfigs(ctx)
		assert.NoError(t, err)
		assert.Len(t, configs, 1)

		// First connection should be marked unhealthy
		assert.False(t, client.connections[0].healthy)
		assert.True(t, client.connections[1].healthy)
		assert.Equal(t, 1, client.currentIdx) // Should have switched to second connection
	})

	t.Run("All_Connections_Fail", func(t *testing.T) {
		// Reset health status
		client.connections[0].healthy = true
		client.connections[1].healthy = true
		client.currentIdx = 0

		// Both connections fail
		mockClient1.EXPECT().
			AllChainConfigs(gomock.Any(), gomock.Any()).
			Return(nil, errors.New("connection refused")).
			AnyTimes()

		mockClient2.EXPECT().
			AllChainConfigs(gomock.Any(), gomock.Any()).
			Return(nil, errors.New("connection refused")).
			AnyTimes()

		_, err := client.GetAllChainConfigs(ctx)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no healthy connections available")
	})
}
