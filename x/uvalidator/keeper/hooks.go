package keeper

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/pushchain/push-chain-node/x/uvalidator/types"
)

// MultiUValidatorHooks allows multiple modules to listen to the same events.
type MultiUValidatorHooks []types.UValidatorHooks

// NewMultiUValidatorHooks creates a new combined hook instance.
func NewMultiUValidatorHooks(hooks ...types.UValidatorHooks) MultiUValidatorHooks {
	return hooks
}

// AfterValidatorAdded calls every hook in the list.
func (mh MultiUValidatorHooks) AfterValidatorAdded(ctx sdk.Context, valAddr sdk.ValAddress) {
	ctx.Logger().Debug("hook: AfterValidatorAdded", "validator", valAddr.String(), "hook_count", len(mh))
	for _, h := range mh {
		h.AfterValidatorAdded(ctx, valAddr)
	}
}

// AfterValidatorRemoved calls every hook in the list.
func (mh MultiUValidatorHooks) AfterValidatorRemoved(ctx sdk.Context, valAddr sdk.ValAddress) {
	ctx.Logger().Debug("hook: AfterValidatorRemoved", "validator", valAddr.String(), "hook_count", len(mh))
	for _, h := range mh {
		h.AfterValidatorRemoved(ctx, valAddr)
	}
}

// AfterValidatorStatusChanged calls every hook in the list.
func (mh MultiUValidatorHooks) AfterValidatorStatusChanged(ctx sdk.Context, valAddr sdk.ValAddress, oldStatus, newStatus types.UVStatus) {
	ctx.Logger().Debug("hook: AfterValidatorStatusChanged",
		"validator", valAddr.String(),
		"old_status", oldStatus.String(),
		"new_status", newStatus.String(),
		"hook_count", len(mh),
	)
	for _, h := range mh {
		h.AfterValidatorStatusChanged(ctx, valAddr, oldStatus, newStatus)
	}
}
