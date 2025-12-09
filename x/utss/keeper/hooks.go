package keeper

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/pushchain/push-chain-node/x/utss/types"
	uvalidatortypes "github.com/pushchain/push-chain-node/x/uvalidator/types"
)

var _ uvalidatortypes.UValidatorHooks = Hooks{}

type Hooks struct {
	k Keeper
}

func (k Keeper) Hooks() Hooks { return Hooks{k} }

// AfterValidatorAdded -> a universal validator has been added to the set
func (h Hooks) AfterValidatorAdded(ctx sdk.Context, valAddr sdk.ValAddress) {
	h.k.Logger().Info("TSS Hook: Universal validator added", "address", valAddr.String())
	h.handleEligibleValidatorSetChange(ctx)
}

// AfterValidatorRemoved -> a universal validator has been removed from the set
func (h Hooks) AfterValidatorRemoved(ctx sdk.Context, valAddr sdk.ValAddress) {
	h.k.Logger().Info("TSS Hook: Universal validator removed", "address", valAddr.String())
	h.handleEligibleValidatorSetChange(ctx)
}

func (h Hooks) AfterValidatorStatusChanged(ctx sdk.Context, valAddr sdk.ValAddress, oldStatus, newStatus uvalidatortypes.UVStatus) {
	h.k.Logger().Info("TSS Hook: Universal validator status changed", "address", oldStatus, newStatus)
}

// handleEligibleValidatorSetChange evaluates the current validator set
// and initiates the appropriate TSS process (KEYGEN when first reaching 2+, QUORUM_CHANGE otherwise).
func (h Hooks) handleEligibleValidatorSetChange(ctx sdk.Context) {
	allUVs, err := h.k.uvalidatorKeeper.GetAllUniversalValidators(ctx)
	if err != nil {
		h.k.Logger().Error("TSS Hook: failed to fetch UVs list", "error", err)
		return
	}

	count := len(allUVs)
	if count < 2 {
		if count == 1 {
			h.k.Logger().Info("TSS Hook: only 1 eligible validator — TSS not possible")
		}
		return
	}

	// Check if we already have a finalized TSS key
	hasExistingKey := false
	_, err = h.k.CurrentTssKey.Get(ctx)
	if err == nil {
		hasExistingKey = true
	}

	processType := types.TssProcessType_TSS_PROCESS_QUORUM_CHANGE
	if !hasExistingKey {
		processType = types.TssProcessType_TSS_PROCESS_KEYGEN
		h.k.Logger().Info("TSS Hook: No existing TSS key -> initiating initial KEYGEN", "eligible_count", count)
	} else {
		h.k.Logger().Info("TSS Hook: Existing TSS key found -> initiating QUORUM_CHANGE reshare", "eligible_count", count)
	}

	// Always attempt to initiate — but NEVER block validator lifecycle
	if err := h.k.InitiateTssKeyProcess(ctx, processType); err != nil {

		h.k.Logger().Error("Failed to initiate TSS process after validator set change",
			"error", err,
			"process_type", processType.String(),
			"eligible_count", count,
		)

		// Emit event
		ctx.EventManager().EmitEvent(
			sdk.NewEvent(
				"tss_process_initiation_failed",
				sdk.NewAttribute("reason", err.Error()),
				sdk.NewAttribute("process_type", processType.String()),
				sdk.NewAttribute("eligible_count", fmt.Sprintf("%d", count)),
				sdk.NewAttribute("block_height", fmt.Sprintf("%d", ctx.BlockHeight())),
			),
		)
	} else {
		h.k.Logger().Info("Successfully initiated TSS process",
			"process_type", processType.String(),
			"eligible_count", count,
		)
	}
}
