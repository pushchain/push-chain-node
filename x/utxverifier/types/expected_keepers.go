package types

import (
	"context"

	"github.com/pushchain/push-chain-node/x/uregistry/types"
)

// UregistryKeeper defines the expected interface for the Uregistry module.
type UregistryKeeper interface {
	GetChainConfig(ctx context.Context, chain string) (types.ChainConfig, error)
}
