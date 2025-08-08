package keeper

import (
	"context"
	"fmt"

	"github.com/pushchain/push-chain-node/x/ue/types"
)

// UpdateChainConfig updates the configuration for a specific chain.
func (k Keeper) UpdateChainConfig(ctx context.Context, chainConfig *types.ChainConfig) error {
	// Check if chain exists
	if has, err := k.ChainConfigs.Has(ctx, chainConfig.Chain); err != nil {
		return err
	} else if !has {
		return fmt.Errorf("chain config for %s does not exist", chainConfig.Chain)
	}

	return k.ChainConfigs.Set(ctx, chainConfig.Chain, *chainConfig)
}
