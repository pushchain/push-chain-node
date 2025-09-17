package common

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/pushchain/push-chain-node/universalClient/config"
	uregistrytypes "github.com/pushchain/push-chain-node/x/uregistry/types"
)

func TestNewBaseChainClient(t *testing.T) {
	t.Run("Create with valid config", func(t *testing.T) {
		config := &uregistrytypes.ChainConfig{
			Chain:          "eip155:1",
			VmType:         uregistrytypes.VmType_EVM,
			GatewayAddress: "0x123",
			Enabled:        &uregistrytypes.ChainEnabled{IsInboundEnabled: true, IsOutboundEnabled: true},
		}

		client := NewBaseChainClient(config, nil)

		assert.NotNil(t, client)
		assert.Equal(t, config, client.config)
		assert.Nil(t, client.ctx)
		assert.Nil(t, client.cancel)
	})

	t.Run("Create with nil config", func(t *testing.T) {
		client := NewBaseChainClient(nil, nil)

		assert.NotNil(t, client)
		assert.Nil(t, client.config)
	})
}

func TestChainID(t *testing.T) {
	t.Run("With valid config", func(t *testing.T) {
		config := &uregistrytypes.ChainConfig{
			Chain: "eip155:1337",
		}
		client := NewBaseChainClient(config, nil)

		chainID := client.ChainID()
		assert.Equal(t, "eip155:1337", chainID)
	})

	t.Run("With nil config", func(t *testing.T) {
		client := NewBaseChainClient(nil, nil)

		chainID := client.ChainID()
		assert.Equal(t, "", chainID)
	})
}

func TestGetConfig(t *testing.T) {
	t.Run("Returns config", func(t *testing.T) {
		config := &uregistrytypes.ChainConfig{
			Chain:          "solana:mainnet",
			VmType:         uregistrytypes.VmType_SVM,
			GatewayAddress: "Sol123",
			Enabled:        &uregistrytypes.ChainEnabled{IsInboundEnabled: true, IsOutboundEnabled: true},
		}
		client := NewBaseChainClient(config, nil)

		returnedConfig := client.GetConfig()
		assert.Equal(t, config, returnedConfig)
	})

	t.Run("Returns nil when no config", func(t *testing.T) {
		client := NewBaseChainClient(nil, nil)

		returnedConfig := client.GetConfig()
		assert.Nil(t, returnedConfig)
	})
}

func TestContextManagement(t *testing.T) {
	t.Run("SetContext creates new context with cancel", func(t *testing.T) {
		client := NewBaseChainClient(nil, nil)
		ctx := context.Background()

		client.SetContext(ctx)

		assert.NotNil(t, client.ctx)
		assert.NotNil(t, client.cancel)
		assert.NotEqual(t, ctx, client.ctx) // Should be a new context
	})

	t.Run("Context returns the set context", func(t *testing.T) {
		client := NewBaseChainClient(nil, nil)
		ctx := context.Background()

		client.SetContext(ctx)
		returnedCtx := client.Context()

		assert.Equal(t, client.ctx, returnedCtx)
	})

	t.Run("Cancel cancels the context", func(t *testing.T) {
		client := NewBaseChainClient(nil, nil)
		ctx := context.Background()

		client.SetContext(ctx)

		// Verify context is not cancelled
		select {
		case <-client.ctx.Done():
			t.Fatal("Context should not be cancelled yet")
		default:
			// Expected
		}

		// Cancel the context
		client.Cancel()

		// Verify context is cancelled
		select {
		case <-client.ctx.Done():
			// Expected
		case <-time.After(100 * time.Millisecond):
			t.Fatal("Context should be cancelled")
		}
	})

	t.Run("Cancel with nil cancel function", func(t *testing.T) {
		client := NewBaseChainClient(nil, nil)

		// Should not panic
		client.Cancel()
	})

	t.Run("Context cancellation propagates from parent", func(t *testing.T) {
		client := NewBaseChainClient(nil, nil)
		parentCtx, parentCancel := context.WithCancel(context.Background())

		client.SetContext(parentCtx)

		// Cancel parent context
		parentCancel()

		// Verify client context is also cancelled
		select {
		case <-client.ctx.Done():
			// Expected
		case <-time.After(100 * time.Millisecond):
			t.Fatal("Client context should be cancelled when parent is cancelled")
		}
	})
}

// TestChainClient is a mock implementation for testing
type TestChainClient struct {
	*BaseChainClient
	started     bool
	stopped     bool
	healthy     bool
	voteHandler VoteHandler
}

func (tc *TestChainClient) Start(ctx context.Context) error {
	tc.started = true
	return nil
}

func (tc *TestChainClient) Stop() error {
	tc.stopped = true
	return nil
}

func (tc *TestChainClient) IsHealthy() bool {
	return tc.healthy
}

// SetVoteHandler sets the vote handler for confirmed transactions
func (tc *TestChainClient) SetVoteHandler(handler VoteHandler) {
	tc.voteHandler = handler
}

// GetGasPrice returns a mock gas price
func (tc *TestChainClient) GetGasPrice(ctx context.Context) (*big.Int, error) {
	return big.NewInt(1000000), nil
}

// GetRPCURLs returns the list of RPC URLs for this chain
func (tc *TestChainClient) GetRPCURLs() []string {
	return tc.BaseChainClient.GetRPCURLs()
}

// GetCleanupSettings returns cleanup settings for this chain
func (tc *TestChainClient) GetCleanupSettings() (cleanupInterval, retentionPeriod int) {
	return tc.BaseChainClient.GetCleanupSettings()
}

// GetGasPriceInterval returns the gas price fetch interval for this chain
func (tc *TestChainClient) GetGasPriceInterval() int {
	return tc.BaseChainClient.GetGasPriceInterval()
}

// GetChainSpecificConfig returns the complete configuration for this chain
func (tc *TestChainClient) GetChainSpecificConfig() *config.ChainSpecificConfig {
	return tc.BaseChainClient.GetChainSpecificConfig()
}

// Implement GatewayOperations interface
func (tc *TestChainClient) GetLatestBlock(ctx context.Context) (uint64, error) {
	return 0, nil
}

func (tc *TestChainClient) WatchGatewayEvents(ctx context.Context, fromBlock uint64) (<-chan *GatewayEvent, error) {
	return nil, nil
}

func (tc *TestChainClient) GetTransactionConfirmations(ctx context.Context, txHash string) (uint64, error) {
	return 0, nil
}

func (tc *TestChainClient) IsConfirmed(ctx context.Context, txHash string) (bool, error) {
	return false, nil
}

func TestChainClientInterface(t *testing.T) {
	// This test verifies that BaseChainClient can be embedded in a struct
	// that implements the ChainClient interface

	// Verify it implements the interface
	var _ ChainClient = (*TestChainClient)(nil)

	client := &TestChainClient{
		BaseChainClient: NewBaseChainClient(&uregistrytypes.ChainConfig{
			Chain: "test:chain",
		}, nil),
		healthy: true,
	}
	// Verify interface methods work through embedding
	assert.Equal(t, "test:chain", client.ChainID())
	assert.NotNil(t, client.GetConfig())
	assert.True(t, client.IsHealthy())
	// Test Start and Stop
	err := client.Start(context.Background())
	assert.NoError(t, err)
	assert.True(t, client.started)

	err = client.Stop()
	assert.NoError(t, err)
	assert.True(t, client.stopped)
}
