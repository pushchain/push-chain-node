package types

import sdk "github.com/cosmos/cosmos-sdk/types"

// UValidatorHooks defines the interface that external modules can implement
// to react to uvalidator lifecycle events.
type UValidatorHooks interface {
	// Triggered when a validator enters PENDING_JOIN (newly added or rejoining)
	AfterValidatorAdded(ctx sdk.Context, valAddr sdk.ValAddress)

	// Triggered when a validator enters PENDING_LEAVE status (starting removal)
	AfterValidatorRemoved(ctx sdk.Context, valAddr sdk.ValAddress)

	// Triggered whenever a validator's status changes between any two valid states
	AfterValidatorStatusChanged(ctx sdk.Context, valAddr sdk.ValAddress, oldStatus, newStatus UVStatus)
}
