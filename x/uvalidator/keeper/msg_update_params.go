package keeper

import (
	"context"

	"github.com/rollchains/pchain/x/uvalidator/types"
)

// updateParams is for updating params collections of the module
func (k Keeper) UpdateParams(ctx context.Context, params types.Params) error {
	return k.Params.Set(ctx, params)
}
