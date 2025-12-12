package keeper

import (
	"context"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/pushchain/push-chain-node/x/uvalidator/types"
)

// UpdateUniversalValidatorStatus updates the UV status from PENDING_LEAVE to ACTIVE
// any other case of status change must fail
func (k Keeper) UpdateUniversalValidatorStatus(
	ctx context.Context,
	coreValidatorAddr string,
	newStatus types.UVStatus,
) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	isOngoingTSS, err := k.utssKeeper.HasOngoingTss(ctx)
	if err != nil {
		return fmt.Errorf("failed to check TSS state: %w", err)
	}
	if isOngoingTSS {
		return fmt.Errorf("cannot update validator status: TSS process is ongoing")
	}

	// Parse core validator address and validate format
	valAddr, err := sdk.ValAddressFromBech32(coreValidatorAddr)
	if err != nil {
		return fmt.Errorf("invalid universal validator address: %w", err)
	}

	// Fetch validator entry
	val, err := k.UniversalValidatorSet.Get(ctx, valAddr)
	if err != nil {
		return fmt.Errorf("universal validator %s not found: %w", coreValidatorAddr, err)
	}

	oldStatus := val.LifecycleInfo.CurrentStatus

	switch val.LifecycleInfo.CurrentStatus {
	case types.UVStatus_UV_STATUS_PENDING_LEAVE:
		if newStatus != types.UVStatus_UV_STATUS_ACTIVE {
			return fmt.Errorf("invalid new status, new status must be ACTIVE")
		}

		// Pending Leave -> Active
		if err := k.UpdateValidatorStatus(ctx, valAddr, newStatus); err != nil {
			return fmt.Errorf("failed to mark validator %s as active: %w", coreValidatorAddr, err)
		}

	default:
		return fmt.Errorf("invalid current status: %s, current status must be PENDING_LEAVE", val.LifecycleInfo.CurrentStatus)
	}

	// ---- Trigger hooks ----
	if k.hooks != nil {
		k.hooks.AfterValidatorStatusChanged(sdkCtx, valAddr, oldStatus, newStatus)
	}

	return nil
}
