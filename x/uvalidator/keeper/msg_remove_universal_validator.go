package keeper

import (
	"context"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/pushchain/push-chain-node/x/uvalidator/types"
)

// RemoveUniversalValidator removes a universal validator from the set and its associated mapping.
// It ensures the validator exists before removal.
func (k Keeper) RemoveUniversalValidator(
	ctx context.Context,
	universalValidatorAddr string,
) error {
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

	switch val.LifecycleInfo.CurrentStatus {
	case types.UVStatus_UV_STATUS_ACTIVE:
		// Active -> Pending Leave
		if err := k.UpdateValidatorStatus(ctx, valAddr, types.UVStatus_UV_STATUS_PENDING_LEAVE); err != nil {
			return fmt.Errorf("failed to mark validator %s as pending leave: %w", universalValidatorAddr, err)
		}

	case types.UVStatus_UV_STATUS_PENDING_JOIN:
		// TODO: check if its present in the current tss process
		// If part of current keygen, reject removal
		// Otherwise mark as inactive
		if err := k.UpdateValidatorStatus(ctx, valAddr, types.UVStatus_UV_STATUS_INACTIVE); err != nil {
			return fmt.Errorf("failed to inactivate validator %s: %w", universalValidatorAddr, err)
		}

	case types.UVStatus_UV_STATUS_PENDING_LEAVE, types.UVStatus_UV_STATUS_INACTIVE:
		return fmt.Errorf("validator %s is already in %s state", universalValidatorAddr, val.LifecycleInfo.CurrentStatus)

	default:
		return fmt.Errorf("invalid lifecycle state for removal: %s", val.LifecycleInfo.CurrentStatus)
	}

	return nil
}
