package types

import (
	"context"

	"github.com/rollchains/pchain/x/ue/types"
)

// EVMKeeper defines the expected interface for the EVM module.
type UeKeeper interface {
	GetChainConfig(ctx context.Context, chainID string) (types.ChainConfig, error)
}
