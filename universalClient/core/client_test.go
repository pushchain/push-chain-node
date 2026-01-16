package core

import (
	"context"
	"testing"

	"github.com/pushchain/push-chain-node/universalClient/chains"
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

func TestChainsTxBuilderFactory(t *testing.T) {
	t.Run("CreateBuilder returns not implemented error", func(t *testing.T) {
		factory := newChainsTxBuilderFactory(nil)

		builder, err := factory.CreateBuilder("eip155:1")
		require.Error(t, err)
		assert.Nil(t, builder)
		assert.Contains(t, err.Error(), "not yet implemented")
		assert.Contains(t, err.Error(), "eip155:1")
	})

	t.Run("CreateBuilder with different chain IDs", func(t *testing.T) {
		factory := newChainsTxBuilderFactory(nil)

		testCases := []string{
			"eip155:1",
			"eip155:97",
			"eip155:137",
			"solana:mainnet",
		}

		for _, chainID := range testCases {
			builder, err := factory.CreateBuilder(chainID)
			require.Error(t, err, "expected error for chain %s", chainID)
			assert.Nil(t, builder)
			assert.Contains(t, err.Error(), chainID)
		}
	})

	t.Run("SupportsChain returns false", func(t *testing.T) {
		factory := newChainsTxBuilderFactory(nil)

		testCases := []string{
			"eip155:1",
			"eip155:97",
			"solana:mainnet",
			"unknown-chain",
		}

		for _, chainID := range testCases {
			supported := factory.SupportsChain(chainID)
			assert.False(t, supported, "expected SupportsChain to return false for %s", chainID)
		}
	})

	t.Run("factory with chains manager", func(t *testing.T) {
		// Create a factory with a non-nil chains manager
		// Even with a chains manager, the implementation returns not implemented
		chainsManager := &chains.Chains{}
		factory := newChainsTxBuilderFactory(chainsManager)

		builder, err := factory.CreateBuilder("eip155:1")
		require.Error(t, err)
		assert.Nil(t, builder)
		assert.Contains(t, err.Error(), "not yet implemented")
	})
}

func TestNewChainsTxBuilderFactory(t *testing.T) {
	t.Run("creates factory with nil chains", func(t *testing.T) {
		factory := newChainsTxBuilderFactory(nil)
		assert.NotNil(t, factory)
	})

	t.Run("creates factory with chains manager", func(t *testing.T) {
		chainsManager := &chains.Chains{}
		factory := newChainsTxBuilderFactory(chainsManager)
		assert.NotNil(t, factory)
	})
}
