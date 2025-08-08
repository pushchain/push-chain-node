package keeper

import (
	"context"

	"github.com/pushchain/push-chain-node/x/ue/types"
)

// updateParams is for updating params collections of the module
func (k Keeper) UpdateParams(ctx context.Context, params types.Params) error {
	return k.Params.Set(ctx, params)
}
