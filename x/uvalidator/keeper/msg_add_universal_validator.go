package keeper

import (
	"context"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/pushchain/push-chain-node/x/uvalidator/types"
)

// new validator -> added as a PENDING_JOIN status
// if existing:
// inactive -> added as a PENDING_JOIN status
// any other status -> revert
// AddUniversalValidator registers or reactivates a core validator as a universal validator.
// It ensures the core validator exists, is bonded, and handles lifecycle reactivation.
func (k Keeper) AddUniversalValidator(
	ctx context.Context,
	coreValidatorAddr, pubkey string,
	networkInfo types.NetworkInfo,
) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Parse core validator address and validate format
	valAddr, err := sdk.ValAddressFromBech32(coreValidatorAddr)
	if err != nil {
		return fmt.Errorf("invalid core validator address: %w", err)
	}

	// Ensure validator exists in staking module
	validator, err := k.stakingKeeper.GetValidator(sdkCtx, valAddr)
	if err != nil {
		return fmt.Errorf("core validator not found: %w", err)
	}

	// Must be bonded to join
	if !validator.IsBonded() {
		return fmt.Errorf("validator %s is not bonded", coreValidatorAddr)
	}

	// Check if already exists
	exists, err := k.UniversalValidatorSet.Has(ctx, valAddr)
	if err != nil {
		return err
	}

	if exists {
		// Fetch existing validator entry
		existingVal, err := k.UniversalValidatorSet.Get(ctx, valAddr)
		if err != nil {
			return fmt.Errorf("failed to fetch existing validator: %w", err)
		}

		switch existingVal.LifecycleInfo.CurrentStatus {
		case types.UVStatus_UV_STATUS_INACTIVE:
			// Reactivate: INACTIVE → PENDING_JOIN
			existingVal.LifecycleInfo.CurrentStatus = types.UVStatus_UV_STATUS_PENDING_JOIN
			existingVal.LifecycleInfo.History = append(existingVal.LifecycleInfo.History, &types.LifecycleEvent{
				Status:      types.UVStatus_UV_STATUS_PENDING_JOIN,
				BlockHeight: sdkCtx.BlockHeight(),
			})
			existingVal.IdentifyInfo.Pubkey = pubkey
			existingVal.NetworkInfo = &networkInfo

			if err := k.UniversalValidatorSet.Set(ctx, valAddr, existingVal); err != nil {
				return fmt.Errorf("failed to reactivate validator: %w", err)
			}
			return nil

		default:
			// Already active or pending — reject
			return fmt.Errorf("validator %s already registered with status %s",
				coreValidatorAddr, existingVal.LifecycleInfo.CurrentStatus)
		}
	}

	// New registration: start as PENDING_JOIN
	initialStatus := types.UVStatus_UV_STATUS_PENDING_JOIN

	uv := types.UniversalValidator{
		IdentifyInfo: &types.IdentityInfo{
			CoreValidatorAddress: coreValidatorAddr,
			Pubkey:               pubkey,
		},
		LifecycleInfo: &types.LifecycleInfo{
			CurrentStatus: initialStatus,
			History: []*types.LifecycleEvent{
				{
					Status:      initialStatus,
					BlockHeight: sdkCtx.BlockHeight(),
				},
			},
		},
		NetworkInfo: &networkInfo,
	}

	// Store new universal validator
	if err := k.UniversalValidatorSet.Set(ctx, valAddr, uv); err != nil {
		return fmt.Errorf("failed to store universal validator: %w", err)
	}

	return nil
}
