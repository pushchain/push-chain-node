package keeper

import (
	"context"
	"fmt"

	"github.com/pushchain/push-chain-node/x/uregistry/types"
)

// UpdateChainConfig updates the configuration for a specific chain.
func (k Keeper) UpdateChainConfig(ctx context.Context, chainConfig *types.ChainConfig) error {
	// Check if chain exists
	if has, err := k.ChainConfigs.Has(ctx, chainConfig.Chain); err != nil {
		return err
	} else if !has {
		return fmt.Errorf("chain config for %s does not exist", chainConfig.Chain)
	}

	if err := k.ChainConfigs.Set(ctx, chainConfig.Chain, *chainConfig); err != nil {
		return err
	}
	k.Logger().Info("chain config updated", "chain", chainConfig.Chain)
	return nil
}
