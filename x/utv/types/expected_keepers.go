package types

import (
	"context"

	"github.com/rollchains/pchain/x/ue/types"
)

// UeKeeper defines the expected interface for the UE module.
type UeKeeper interface {
	GetChainConfig(ctx context.Context, chain string) (types.ChainConfig, error)
}
