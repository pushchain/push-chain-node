package types

import (
	"context"

	"github.com/rollchains/pchain/x/uregistry/types"
)

// UregistryKeeper defines the expected interface for the UE module.
type UregistryKeeper interface {
	GetChainConfig(ctx context.Context, chain string) (types.ChainConfig, error)
}
