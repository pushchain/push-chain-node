package keeper

import (
	"context"
	"fmt"

	"github.com/rollchains/pchain/x/uregistry/types"
)

// UpdateTokenConfig updates an existing token configuration in the uregistry.
func (k Keeper) UpdateTokenConfig(ctx context.Context, tokenConfig *types.TokenConfig) error {
	storageKey := types.GetTokenConfigsStorageKey(tokenConfig.Chain, tokenConfig.Address)

	// Check if the token config exists
	if has, err := k.TokenConfigs.Has(ctx, storageKey); err != nil {
		return err
	} else if !has {
		return fmt.Errorf("token config for %s on chain %s does not exist", tokenConfig.Address, tokenConfig.Chain)
	}

	return k.TokenConfigs.Set(ctx, storageKey, *tokenConfig)
}
