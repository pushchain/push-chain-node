package keeper

import (
	"context"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/pushchain/push-chain-node/x/uvalidator/types"
)

// AddUniversalValidator registers a core validator as a universal validator.
// It ensures the core validator exists, is bonded, and not already present.
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

	// Ensure the core validator exists in the staking module
	validator, err := k.stakingKeeper.GetValidator(sdkCtx, valAddr)
	if err != nil {
		return fmt.Errorf("core validator not found: %w", err)
	}

	// Check that the validator is in bonded status
	if !validator.IsBonded() {
		return fmt.Errorf("validator %s is not bonded", coreValidatorAddr)
	}

	// Revert if already present
	if exists, err := k.UniversalValidatorSet.Has(ctx, valAddr); err != nil {
		return err
	} else if exists {
		return fmt.Errorf("validator %s already registered", coreValidatorAddr)
	}

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

	// Add universal validator to the set
	if err := k.UniversalValidatorSet.Set(ctx, valAddr, uv); err != nil {
		return fmt.Errorf("failed to store universal validator: %w", err)
	}

	return nil
}
