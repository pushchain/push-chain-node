package keeper

import (
	"context"
	"fmt"

	"github.com/pushchain/push-chain-node/x/uregistry/types"
)

// addChainConfig is for adding a new chain configuration
func (k Keeper) AddChainConfig(ctx context.Context, chainConfig *types.ChainConfig) error {
	// Check if chain already exists
	if has, err := k.ChainConfigs.Has(ctx, chainConfig.Chain); err != nil {
		return err
	} else if has {
		return fmt.Errorf("chain config for %s already exists", chainConfig.Chain)
	}

	return k.ChainConfigs.Set(ctx, chainConfig.Chain, *chainConfig)
}
