package keeper

import (
	"context"

	"github.com/rollchains/pchain/x/ue/types"
)

// updateParams is for updating admin params collections of the module
func (k Keeper) updateAdminParams(ctx context.Context, adminParams types.AdminParams) error {
	return k.AdminParams.Set(ctx, adminParams)
}
