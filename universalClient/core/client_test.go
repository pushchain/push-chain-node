package core

import (
	"context"
	"testing"

	"github.com/pushchain/push-chain-node/universalClient/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewUniversalClient(t *testing.T) {
	t.Run("fails with nil config", func(t *testing.T) {
		ctx := context.Background()

		client, err := NewUniversalClient(ctx, nil)
		require.Error(t, err)
		assert.Nil(t, client)
		assert.Contains(t, err.Error(), "Config is nil")
	})

	t.Run("fails with empty PushChainGRPCURLs", func(t *testing.T) {
		ctx := context.Background()

		cfg := &config.Config{
			PushChainGRPCURLs: []string{},
			LogLevel:          1,
			LogFormat:         "console",
		}

		client, err := NewUniversalClient(ctx, cfg)
		require.Error(t, err)
		assert.Nil(t, client)
		assert.Contains(t, err.Error(), "failed to create pushcore client")
		assert.Contains(t, err.Error(), "at least one gRPC URL is required")
	})

	t.Run("fails with nil PushChainGRPCURLs", func(t *testing.T) {
		ctx := context.Background()

		cfg := &config.Config{
			PushChainGRPCURLs: nil,
			LogLevel:          1,
			LogFormat:         "console",
		}

		client, err := NewUniversalClient(ctx, cfg)
		require.Error(t, err)
		assert.Nil(t, client)
		assert.Contains(t, err.Error(), "failed to create pushcore client")
	})

	t.Run("fails with invalid valoper address", func(t *testing.T) {
		ctx := context.Background()

		cfg := &config.Config{
			PushChainGRPCURLs:  []string{"localhost:9090"},
			PushValoperAddress: "invalid-valoper-address",
			LogLevel:           1,
			LogFormat:          "console",
		}

		client, err := NewUniversalClient(ctx, cfg)
		require.Error(t, err)
		assert.Nil(t, client)
		assert.Contains(t, err.Error(), "failed to parse valoper address")
	})
}

func TestUniversalClientStruct(t *testing.T) {
	t.Run("struct has expected fields", func(t *testing.T) {
		// Verify the UniversalClient struct has all expected fields
		uc := &UniversalClient{}
		assert.Nil(t, uc.ctx)
		assert.Nil(t, uc.config)
		assert.Nil(t, uc.queryServer)
		assert.Nil(t, uc.pushCore)
		assert.Nil(t, uc.pushSigner)
		assert.Nil(t, uc.chains)
		assert.Nil(t, uc.tssNode)
	})
}

// Note: Factory tests removed as OutboundTxBuilderFactory has been replaced
// with direct chain client access via Chains.GetClient() and ChainClient.GetTxBuilder()
