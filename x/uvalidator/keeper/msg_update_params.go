package keeper

import (
	"context"

	"github.com/pushchain/push-chain-node/x/uvalidator/types"
)

// updateParams is for updating params collections of the module
func (k Keeper) UpdateParams(ctx context.Context, params types.Params) error {
	k.Logger().Info("updating module params", "admin", params.Admin)
	if err := k.Params.Set(ctx, params); err != nil {
		return err
	}
	k.Logger().Info("module params updated successfully", "admin", params.Admin)
	return nil
}
