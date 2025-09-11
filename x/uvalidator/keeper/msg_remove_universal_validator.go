package keeper

import (
	"context"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
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

	// Check if the universal validator is in the set
	exists, err := k.UniversalValidatorSet.Has(ctx, valAddr)
	if err != nil {
		return fmt.Errorf("failed to check universal validator existence: %w", err)
	}
	if !exists {
		return fmt.Errorf("universal validator %s is not registered", universalValidatorAddr)
	}

	// Remove from the set
	if err := k.UniversalValidatorSet.Remove(ctx, valAddr); err != nil {
		return fmt.Errorf("failed to remove universal validator from set: %w", err)
	}

	return nil
}
