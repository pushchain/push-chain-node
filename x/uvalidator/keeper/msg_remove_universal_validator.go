package keeper

import (
	"context"
	"fmt"
)

// RemoveUniversalValidator removes a universal validator from the set and its associated mapping.
// It ensures the validator exists before removal.
func (k Keeper) RemoveUniversalValidator(
	ctx context.Context,
	universalValidatorAddr string,
) error {
	// Check if the universal validator is in the set
	exists, err := k.UniversalValidatorSet.Has(ctx, universalValidatorAddr)
	if err != nil {
		return fmt.Errorf("failed to check universal validator existence: %w", err)
	}
	if !exists {
		return fmt.Errorf("universal validator %s is not registered", universalValidatorAddr)
	}

	// Remove from the set
	if err := k.UniversalValidatorSet.Remove(ctx, universalValidatorAddr); err != nil {
		return fmt.Errorf("failed to remove universal validator from set: %w", err)
	}

	// Scan through CoreToUniversal to remove the entry pointing to this universal address
	if err := k.RemoveCoreToUniversalMappingByUniversalAddr(ctx, universalValidatorAddr); err != nil {
		return err
	}
	return nil
}
