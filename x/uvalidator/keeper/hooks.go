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
