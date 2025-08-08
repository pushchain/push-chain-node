package types

import (
	"context"

	"github.com/pushchain/push-chain-node/x/uexecutor/types"
)

// UexecutorKeeper defines the expected interface for the Uexecutor module.
type UexecutorKeeper interface {
	GetChainConfig(ctx context.Context, chain string) (types.ChainConfig, error)
}
