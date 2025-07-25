package keeper

import (
	"context"
	"fmt"

	"github.com/rollchains/pchain/x/registry/types"
)

// AddTokenConfig adds a new token configuration to the registry.
func (k Keeper) AddTokenConfig(ctx context.Context, tokenConfig *types.TokenConfig) error {
	storageKey := types.GetTokenConfigsStorageKey(tokenConfig.Chain, tokenConfig.Address)

	// Check if the token config already exists
	if has, err := k.TokenConfigs.Has(ctx, storageKey); err != nil {
		return err
	} else if has {
		return fmt.Errorf("token config for %s on chain %s already exists", tokenConfig.Address, tokenConfig.Chain)
	}

	return k.TokenConfigs.Set(ctx, storageKey, *tokenConfig)
}
