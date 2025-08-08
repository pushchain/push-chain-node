package types

import (
	"context"

	"github.com/pushchain/push-chain-node/x/ue/types"
)

// UeKeeper defines the expected interface for the UE module.
type UeKeeper interface {
	GetChainConfig(ctx context.Context, chain string) (types.ChainConfig, error)
}
