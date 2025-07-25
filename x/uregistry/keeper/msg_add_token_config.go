package keeper

import (
	"context"
	"fmt"

	"github.com/rollchains/pchain/x/uregistry/types"
)

// AddTokenConfig adds a new token configuration to the uregistry.
func (k Keeper) AddTokenConfig(ctx context.Context, tokenConfig *types.TokenConfig) error {
	// Ensure the chain exists
	if _, err := k.GetChainConfig(ctx, tokenConfig.Chain); err != nil {
		return fmt.Errorf("chain %s is not supported: %w", tokenConfig.Chain, err)
	}

	// More efficient check for existing token config
	storageKey := types.GetTokenConfigsStorageKey(tokenConfig.Chain, tokenConfig.Address)
	has, err := k.TokenConfigs.Has(ctx, storageKey)
	if err != nil {
		return err
	}
	if has {
		return fmt.Errorf("token config for %s on chain %s already exists", tokenConfig.Address, tokenConfig.Chain)
	}

	// Set the new token config
	return k.TokenConfigs.Set(ctx, storageKey, *tokenConfig)
}
