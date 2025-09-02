package keeper

import (
	"context"

<<<<<<< HEAD
	"github.com/pushchain/push-chain-node/x/uvalidator/types"
=======
	"github.com/rollchains/pchain/x/uvalidator/types"
>>>>>>> feat/universal-validator
)

// updateParams is for updating params collections of the module
func (k Keeper) UpdateParams(ctx context.Context, params types.Params) error {
	return k.Params.Set(ctx, params)
}
