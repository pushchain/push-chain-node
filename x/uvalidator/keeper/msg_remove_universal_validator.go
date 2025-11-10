package keeper

import (
	"context"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/pushchain/push-chain-node/x/uvalidator/types"
)

// RemoveUniversalValidator handles universal validator removal lifecycle:
//   - ACTIVE -> PENDING_LEAVE
//   - PENDING_JOIN ->
//   - if in current TSS process → revert (keygen ongoing)
//   - if not in current TSS process → INACTIVE
//
// It ensures the validator exists before removal and triggers hooks on status change.
func (k Keeper) RemoveUniversalValidator(
	ctx context.Context,
	universalValidatorAddr string,
) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Parse core validator address and validate format
	valAddr, err := sdk.ValAddressFromBech32(universalValidatorAddr)
	if err != nil {
		return fmt.Errorf("invalid universal validator address: %w", err)
	}

	// Fetch validator entry
	val, err := k.UniversalValidatorSet.Get(ctx, valAddr)
	if err != nil {
		return fmt.Errorf("universal validator %s not found: %w", universalValidatorAddr, err)
	}

	oldStatus := val.LifecycleInfo.CurrentStatus
	var newStatus types.UVStatus

	switch val.LifecycleInfo.CurrentStatus {
	case types.UVStatus_UV_STATUS_ACTIVE:
		// Active -> Pending Leave
		if err := k.UpdateValidatorStatus(ctx, valAddr, types.UVStatus_UV_STATUS_PENDING_LEAVE); err != nil {
			return fmt.Errorf("failed to mark validator %s as pending leave: %w", universalValidatorAddr, err)
		}

		newStatus = types.UVStatus_UV_STATUS_PENDING_LEAVE

	case types.UVStatus_UV_STATUS_PENDING_JOIN:
		// TODO: check if its present in the current tss process
		// If part of current keygen, reject removal
		// Otherwise mark as inactive
		if err := k.UpdateValidatorStatus(ctx, valAddr, types.UVStatus_UV_STATUS_INACTIVE); err != nil {
			return fmt.Errorf("failed to inactivate validator %s: %w", universalValidatorAddr, err)
		}

		newStatus = types.UVStatus_UV_STATUS_INACTIVE

	case types.UVStatus_UV_STATUS_PENDING_LEAVE, types.UVStatus_UV_STATUS_INACTIVE:
		return fmt.Errorf("validator %s is already in %s state", universalValidatorAddr, val.LifecycleInfo.CurrentStatus)

	default:
		return fmt.Errorf("invalid lifecycle state for removal: %s", val.LifecycleInfo.CurrentStatus)
	}

	// ---- Trigger hooks ----
	if k.hooks != nil {
		k.hooks.AfterValidatorStatusChanged(sdkCtx, valAddr, oldStatus, newStatus)
		k.hooks.AfterValidatorRemoved(sdkCtx, valAddr)
	}

	return nil
}
