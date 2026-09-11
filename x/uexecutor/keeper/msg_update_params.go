package keeper

import (
	"context"

	"github.com/pushchain/push-chain-node/x/uexecutor/types"
)

// GetParams returns the current module parameters.
func (k Keeper) GetParams(ctx context.Context) (types.Params, error) {
	return k.Params.Get(ctx)
}

// updateParams is for updating params collections of the module
func (k Keeper) UpdateParams(ctx context.Context, params types.Params) error {
	if err := params.ValidateBasic(); err != nil {
		return err
	}

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
