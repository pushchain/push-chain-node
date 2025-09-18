package core

import (
	"context"
	"testing"

	"github.com/pushchain/push-chain-node/universalClient/config"
	"github.com/pushchain/push-chain-node/universalClient/db"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
)

func TestNewUniversalClient(t *testing.T) {
	t.Run("fails with empty PushChainGRPCURLs", func(t *testing.T) {
		ctx := context.Background()
		logger := zerolog.Nop()

		cfg := &config.Config{
			PushChainGRPCURLs: []string{},
		}
		// This test can run - it should fail before attempting any connections
		dbManager := db.NewChainDBManager("", logger, cfg)

		client, err := NewUniversalClient(ctx, logger, dbManager, cfg)
		assert.Error(t, err)
		assert.Nil(t, client)
		assert.Contains(t, err.Error(), "PushChainGRPCURLs is required")
	})
}