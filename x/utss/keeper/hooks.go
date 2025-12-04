package keeper

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/pushchain/push-chain-node/x/utss/types"
	uvalidatortypes "github.com/pushchain/push-chain-node/x/uvalidator/types"
)

var _ uvalidatortypes.UValidatorHooks = Hooks{}

type Hooks struct {
	k Keeper
}

func (k Keeper) Hooks() Hooks { return Hooks{k} }

func (h Hooks) AfterValidatorAdded(ctx sdk.Context, valAddr sdk.ValAddress) {
	h.k.Logger().Info("TSS Hook: Universal validator added", "address", valAddr.String())

	if err := h.k.InitiateTssKeyProcess(ctx, types.TssProcessType_TSS_PROCESS_QUORUM_CHANGE); err != nil {
		h.k.Logger().Error("Failed to initiate TSS key process in hook", "error", err)
		ctx.EventManager().EmitEvent(
			sdk.NewEvent(
				"tss_process_initiation_failed",
				sdk.NewAttribute("reason", err.Error()),
				sdk.NewAttribute("validator", valAddr.String()),
			),
		)
	}
}

func (h Hooks) AfterValidatorRemoved(ctx sdk.Context, valAddr sdk.ValAddress) {
	h.k.Logger().Info("TSS Hook: Universal validator removed", "address", valAddr.String())

	if err := h.k.InitiateTssKeyProcess(ctx, types.TssProcessType_TSS_PROCESS_QUORUM_CHANGE); err != nil {
		h.k.Logger().Error("Failed to initiate TSS key process in hook", "error", err)
		ctx.EventManager().EmitEvent(
			sdk.NewEvent(
				"tss_process_initiation_failed",
				sdk.NewAttribute("reason", err.Error()),
				sdk.NewAttribute("validator", valAddr.String()),
			),
		)
	}
}

func (h Hooks) AfterValidatorStatusChanged(ctx sdk.Context, valAddr sdk.ValAddress, oldStatus, newStatus uvalidatortypes.UVStatus) {
	h.k.Logger().Info("TSS Hook: Universal validator status changed", "address", oldStatus, newStatus)
}
