package keeper

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	uvalidatortypes "github.com/pushchain/push-chain-node/x/uvalidator/types"
)

var _ uvalidatortypes.UValidatorHooks = Hooks{}

type Hooks struct {
	k Keeper
}

func (k Keeper) Hooks() Hooks { return Hooks{k} }

func (h Hooks) AfterValidatorAdded(ctx sdk.Context, valAddr sdk.ValAddress) {
	// Example: trigger new TSS participation setup
	h.k.Logger().Info("TSS Hook: Universal validator added", "address", valAddr.String())

	// you can enqueue this validator for keygen participation here
}

func (h Hooks) AfterValidatorRemoved(ctx sdk.Context, valAddr sdk.ValAddress) {
	h.k.Logger().Info("TSS Hook: Universal validator removed", "address", valAddr.String())

	// maybe mark as inactive in current TSS session
}

func (h Hooks) AfterValidatorStatusChanged(ctx sdk.Context, valAddr sdk.ValAddress, oldStatus, newStatus uvalidatortypes.UVStatus) {
	h.k.Logger().Info("TSS Hook: Universal validator status changed", "address", oldStatus, newStatus)

	// maybe mark as inactive in current TSS session
}
