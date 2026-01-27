package keeper

import (
	"context"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/pushchain/push-chain-node/x/uvalidator/types"
)

// UpdateUniversalValidator updates the metadata of the registered universal validator
func (k Keeper) UpdateUniversalValidator(
	ctx context.Context,
	coreValidatorAddr string,
	networkInfo types.NetworkInfo,
) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Parse core validator address and validate format
	valAddr, err := sdk.ValAddressFromBech32(coreValidatorAddr)
	if err != nil {
		return fmt.Errorf("invalid core validator address: %w", err)
	}

	// Ensure validator exists in staking module
	_, err = k.StakingKeeper.GetValidator(sdkCtx, valAddr)
	if err != nil {
		return fmt.Errorf("core validator not found: %w", err)
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
	existingVal.NetworkInfo = &networkInfo

	// Save updated entry
	if err := k.UniversalValidatorSet.Set(ctx, valAddr, existingVal); err != nil {
		return fmt.Errorf("failed to update universal validator: %w", err)
	}

	return nil
}
