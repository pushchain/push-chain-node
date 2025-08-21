package keeper

import (
	"context"
	"fmt"
)

// UpdateUniversalValidator updates the universal validator mapped to a given core validator.
// It validates existence, and ensures no duplication.
func (k Keeper) UpdateUniversalValidator(
	ctx context.Context,
	coreValidatorAddr string,
	newUniversalValidatorAddr string,
) error {
	// @dev: No need to check if the validator is bonded
	// already checked when validator was added

	// Check that the core validator already has a universal validator mapped
	oldUniversal, found, err := k.GetCoreToUniversal(ctx, coreValidatorAddr)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("no universal validator mapping exists for core validator %s", coreValidatorAddr)
	}

	// Check if new universal validator is already in the set
	if exists, err := k.HasUniversalValidatorInSet(ctx, newUniversalValidatorAddr); err != nil {
		return err
	} else if exists {
		return fmt.Errorf("universal validator %s is already registered", newUniversalValidatorAddr)
	}

	// Remove old universal validator from the set
	if err := k.RemoveUniversalValidatorFromSet(ctx, oldUniversal); err != nil {
		return fmt.Errorf("failed to remove old universal validator: %w", err)
	}

	// Update mapping with the new universal validator
	if err := k.SetCoreToUniversal(ctx, coreValidatorAddr, newUniversalValidatorAddr); err != nil {
		return fmt.Errorf("failed to update core to universal mapping: %w", err)
	}

	// Add new universal validator to the set
	if err := k.AddUniversalValidatorToSet(ctx, newUniversalValidatorAddr); err != nil {
		return fmt.Errorf("failed to add new universal validator: %w", err)
	}

	return nil
}
