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
//   - if in current TSS process (ongoing) → revert (keygen ongoing)
//   - if not in current TSS process (ongoing) → INACTIVE
//
// It ensures the validator exists before removal and triggers hooks on status change.
func (k Keeper) RemoveUniversalValidator(
	ctx context.Context,
	universalValidatorAddr string,
) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	k.Logger().Info("removing universal validator", "validator", universalValidatorAddr)

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

	k.Logger().Debug("universal validator removal: current status",
		"validator", universalValidatorAddr,
		"current_status", oldStatus.String(),
	)

	switch val.LifecycleInfo.CurrentStatus {
	case types.UVStatus_UV_STATUS_ACTIVE:
		k.Logger().Info("transitioning validator to PENDING_LEAVE",
			"validator", universalValidatorAddr,
			"old_status", oldStatus.String(),
			"new_status", types.UVStatus_UV_STATUS_PENDING_LEAVE.String(),
		)
		// Active -> Pending Leave
		if err := k.UpdateValidatorStatus(ctx, valAddr, types.UVStatus_UV_STATUS_PENDING_LEAVE); err != nil {
			return fmt.Errorf("failed to mark validator %s as pending leave: %w", universalValidatorAddr, err)
		}

		newStatus = types.UVStatus_UV_STATUS_PENDING_LEAVE

	case types.UVStatus_UV_STATUS_PENDING_JOIN:
		// Check if validator is part of the current TSS process
		currentParticipants, err := k.UtssKeeper.GetCurrentTssParticipants(ctx)
		if err != nil {
			return fmt.Errorf("failed to fetch current TSS participants: %w", err)
		}

		isParticipant := false
		for _, p := range currentParticipants {
			if p == universalValidatorAddr {
				isParticipant = true
				break
			}
		}

		// If part of current keygen, reject removal
		if isParticipant {
			k.Logger().Warn("cannot remove validator: active TSS keygen participant",
				"validator", universalValidatorAddr,
			)
			return fmt.Errorf("validator %s is part of the current TSS process and cannot be removed", universalValidatorAddr)
		}

		k.Logger().Info("transitioning PENDING_JOIN validator to INACTIVE",
			"validator", universalValidatorAddr,
			"old_status", oldStatus.String(),
			"new_status", types.UVStatus_UV_STATUS_INACTIVE.String(),
		)

		// Otherwise, mark as inactive
		if err := k.UpdateValidatorStatus(ctx, valAddr, types.UVStatus_UV_STATUS_INACTIVE); err != nil {
			return fmt.Errorf("failed to inactivate validator %s: %w", universalValidatorAddr, err)
		}
		newStatus = types.UVStatus_UV_STATUS_INACTIVE

	case types.UVStatus_UV_STATUS_PENDING_LEAVE, types.UVStatus_UV_STATUS_INACTIVE:
		k.Logger().Warn("cannot remove validator: already in terminal state",
			"validator", universalValidatorAddr,
			"current_status", val.LifecycleInfo.CurrentStatus.String(),
		)
		return fmt.Errorf("validator %s is already in %s state", universalValidatorAddr, val.LifecycleInfo.CurrentStatus)

	default:
		k.Logger().Warn("cannot remove validator: invalid lifecycle state",
			"validator", universalValidatorAddr,
			"current_status", val.LifecycleInfo.CurrentStatus.String(),
		)
		return fmt.Errorf("invalid lifecycle state for removal: %s", val.LifecycleInfo.CurrentStatus)
	}

	k.Logger().Info("universal validator removal initiated",
		"validator", universalValidatorAddr,
		"old_status", oldStatus.String(),
		"new_status", newStatus.String(),
	)

	// ---- Trigger hooks ----
	if k.hooks != nil {
		k.hooks.AfterValidatorStatusChanged(sdkCtx, valAddr, oldStatus, newStatus)
		k.hooks.AfterValidatorRemoved(sdkCtx, valAddr)
	}

	return nil
}
