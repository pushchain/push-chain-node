package keeper

import (
	"context"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// AddUniversalValidator registers a new universal validator for a given core validator.
// It ensures the core validator exists, is bonded, and no existing mapping exists.
func (k Keeper) AddUniversalValidator(
	ctx context.Context,
	coreValidatorAddr string,
	universalValidatorAddr string,
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

	// Prevent duplicate mapping of core validator
	if exists, err := k.HasCoreToUniversal(ctx, coreValidatorAddr); err != nil {
		return err
	} else if exists {
		return fmt.Errorf("core validator already mapped to a universal validator")
	}

	// Prevent duplicate registration of universal validator
	if exists, err := k.HasUniversalValidatorInSet(ctx, universalValidatorAddr); err != nil {
		return err
	} else if exists {
		return fmt.Errorf("universal validator already registered")
	}

	// Save the core-to-universal mapping
	if err := k.SetCoreToUniversal(ctx, coreValidatorAddr, universalValidatorAddr); err != nil {
		return err
	}

	// Add universal validator to the set
	if err := k.AddUniversalValidatorToSet(ctx, universalValidatorAddr); err != nil {
		return err
	}

	return nil
}
