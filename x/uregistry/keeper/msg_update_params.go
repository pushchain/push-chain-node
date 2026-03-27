package keeper

import (
	"context"

	"github.com/pushchain/push-chain-node/x/uregistry/types"
)

// updateParams is for updating params collections of the module
func (k Keeper) UpdateParams(ctx context.Context, params types.Params) error {
	if err := k.Params.Set(ctx, params); err != nil {
		return err
	}
	k.Logger().Info("uregistry params updated",
		"admin", params.Admin,
	)
	return nil
}
