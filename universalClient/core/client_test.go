package core

import (
	"context"
	"testing"

	"github.com/pushchain/push-chain-node/universalClient/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewUniversalClient(t *testing.T) {
	t.Run("nil config", func(t *testing.T) {
		client, err := NewUniversalClient(context.Background(), nil)
		require.Error(t, err)
		assert.Nil(t, client)
		assert.Contains(t, err.Error(), "config is nil")
	})

	t.Run("empty valoper address", func(t *testing.T) {
		cfg := &config.Config{
			PushChainGRPCURLs: []string{"localhost:9090"},
			LogLevel:          1,
			LogFormat:         "console",
		}

		client, err := NewUniversalClient(context.Background(), cfg)
		require.Error(t, err)
		assert.Nil(t, client)
		assert.Contains(t, err.Error(), "push_valoper_address is required")
	})

	t.Run("empty gRPC URLs", func(t *testing.T) {
		cfg := &config.Config{
			PushChainGRPCURLs: []string{},
			LogLevel:          1,
			LogFormat:         "console",
		}

		client, err := NewUniversalClient(context.Background(), cfg)
		require.Error(t, err)
		assert.Nil(t, client)
		assert.Contains(t, err.Error(), "at least one gRPC URL is required")
	})

	t.Run("invalid valoper address", func(t *testing.T) {
		cfg := &config.Config{
			PushChainGRPCURLs:  []string{"localhost:9090"},
			PushValoperAddress: "invalid-valoper-address",
			LogLevel:           1,
			LogFormat:          "console",
		}

		client, err := NewUniversalClient(context.Background(), cfg)
		require.Error(t, err)
		assert.Nil(t, client)
		assert.Contains(t, err.Error(), "failed to parse valoper address")
	})
}

func TestValoperToAccountAddr(t *testing.T) {
	t.Run("empty valoper returns error", func(t *testing.T) {
		_, err := valoperToAccountAddr("")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "push_valoper_address is required")
	})

	t.Run("invalid valoper returns error", func(t *testing.T) {
		_, err := valoperToAccountAddr("garbage")
		require.Error(t, err)
	})
}

func TestSanitizeForFilename(t *testing.T) {
	assert.Equal(t, "eip155_1", sanitizeForFilename("eip155:1"))
	assert.Equal(t, "push_42101-1", sanitizeForFilename("push_42101-1"))
	assert.Equal(t, "solana_EtWTRABZaYq6", sanitizeForFilename("solana:EtWTRABZaYq6"))
}
