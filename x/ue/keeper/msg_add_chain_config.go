package keeper

import (
	"context"
	"fmt"

	"github.com/rollchains/pchain/x/ue/types"
)

// updateParams is for updating params collections of the module
func (k Keeper) addChainConfig(ctx context.Context, chainConfig *types.ChainConfig) error {
	// Check if chain ID already exists
	if has, err := k.ChainConfigs.Has(ctx, chainConfig.ChainId); err != nil {
		return err
	} else if has {
		return fmt.Errorf("chain config for %s already exists", chainConfig.ChainId)
	}

	return k.ChainConfigs.Set(ctx, chainConfig.ChainId, *chainConfig)
}
