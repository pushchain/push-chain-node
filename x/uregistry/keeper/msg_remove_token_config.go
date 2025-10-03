package keeper

import (
	"context"
	"fmt"

	"github.com/pushchain/push-chain-node/x/uregistry/types"
)

// RemoveTokenConfig removes an existing token configuration in the uregistry.
func (k Keeper) RemoveTokenConfig(ctx context.Context, chain, tokenAddress string) error {
	storageKey := types.GetTokenConfigsStorageKey(chain, tokenAddress)

	// Check if the token config exists
	if has, err := k.TokenConfigs.Has(ctx, storageKey); err != nil {
		return err
	} else if !has {
		return fmt.Errorf("token config for %s on chain %s does not exist", tokenAddress, chain)
	}

	return k.TokenConfigs.Remove(ctx, storageKey)
}
