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
// UpdateUniversalValidator registers or reactivates a core validator as a universal validator.
// It ensures the core validator exists, is bonded, and handles lifecycle reactivation.
func (k Keeper) UpdateUniversalValidator(
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
	if !exists {
		return fmt.Errorf("validator %s does not exist", coreValidatorAddr)
	}

	// Fetch existing universal validator
	existingVal, err := k.UniversalValidatorSet.Get(ctx, valAddr)
	if err != nil {
		return fmt.Errorf("failed to fetch existing universal validator: %w", err)
	}

	// Update only metadata
	existingVal.IdentifyInfo.Pubkey = pubkey
	existingVal.NetworkInfo = &networkInfo

	// Save updated entry
	if err := k.UniversalValidatorSet.Set(ctx, valAddr, existingVal); err != nil {
		return fmt.Errorf("failed to update universal validator: %w", err)
	}

	return nil
}
