package keeper

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	uvalidatortypes "github.com/pushchain/push-chain-node/x/uvalidator/types"
)

// UValidatorHooks implements uvalidatortypes.UValidatorHooks for the uexecutor module.
// It prunes removed validators' votes from GasPrices and ChainMetas.
type UValidatorHooks struct {
	k Keeper
}

func NewUValidatorHooks(k Keeper) UValidatorHooks {
	return UValidatorHooks{k: k}
}

var _ uvalidatortypes.UValidatorHooks = UValidatorHooks{}

func (h UValidatorHooks) AfterValidatorAdded(ctx sdk.Context, valAddr sdk.ValAddress) {}

func (h UValidatorHooks) AfterValidatorRemoved(ctx sdk.Context, valAddr sdk.ValAddress) {
	h.k.PruneValidatorVotes(ctx, valAddr.String())
}

func (h UValidatorHooks) AfterValidatorStatusChanged(ctx sdk.Context, valAddr sdk.ValAddress, oldStatus, newStatus uvalidatortypes.UVStatus) {
}
