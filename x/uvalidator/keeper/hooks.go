package keeper

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/pushchain/push-chain-node/x/uvalidator/types"
)

// UValidatorHooks defines the interface that external modules can implement
// to react to uvalidator lifecycle events.
type UValidatorHooks interface {
	// Triggered when a validator enters PENDING_JOIN (newly added or rejoining)
	AfterValidatorAdded(ctx sdk.Context, valAddr sdk.ValAddress)

	// Triggered when a validator enters PENDING_LEAVE status (starting removal)
	AfterValidatorRemoved(ctx sdk.Context, valAddr sdk.ValAddress)

	// Triggered whenever a validator's status changes between any two valid states
	AfterValidatorStatusChanged(ctx sdk.Context, valAddr sdk.ValAddress, oldStatus, newStatus types.UVStatus)
}

// MultiUValidatorHooks allows multiple modules to listen to the same events.
type MultiUValidatorHooks []UValidatorHooks

// NewMultiUValidatorHooks creates a new combined hook instance.
func NewMultiUValidatorHooks(hooks ...UValidatorHooks) MultiUValidatorHooks {
	return hooks
}

// AfterValidatorAdded calls every hook in the list.
func (mh MultiUValidatorHooks) AfterValidatorAdded(ctx sdk.Context, valAddr sdk.ValAddress) {
	for _, h := range mh {
		h.AfterValidatorAdded(ctx, valAddr)
	}
}

// AfterValidatorRemoved calls every hook in the list.
func (mh MultiUValidatorHooks) AfterValidatorRemoved(ctx sdk.Context, valAddr sdk.ValAddress) {
	for _, h := range mh {
		h.AfterValidatorRemoved(ctx, valAddr)
	}
}

// AfterValidatorStatusChanged calls every hook in the list.
func (mh MultiUValidatorHooks) AfterValidatorStatusChanged(ctx sdk.Context, valAddr sdk.ValAddress, oldStatus, newStatus types.UVStatus) {
	for _, h := range mh {
		h.AfterValidatorStatusChanged(ctx, valAddr, oldStatus, newStatus)
	}
}
