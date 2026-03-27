package keeper

import (
	"context"

	"github.com/pushchain/push-chain-node/x/uexecutor/types"
)

// updateParams is for updating params collections of the module
func (k Keeper) UpdateParams(ctx context.Context, params types.Params) error {
	oldParams, err := k.Params.Get(ctx)
	if err == nil {
		k.Logger().Info("params updated",
			"old_params", oldParams.String(),
			"new_params", params.String(),
		)
	} else {
		k.Logger().Info("params set (initial)", "params", params.String())
	}
	return k.Params.Set(ctx, params)
}
