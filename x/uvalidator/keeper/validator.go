package keeper

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/pushchain/push-chain-node/x/uvalidator/types"
)

// GetAllUniversalValidators returns all validators in the UniversalValidatorSet.
func (k Keeper) GetAllUniversalValidators(ctx context.Context) ([]types.UniversalValidator, error) {
	var vals []types.UniversalValidator

	err := k.UniversalValidatorSet.Walk(ctx, nil, func(addr sdk.ValAddress, val types.UniversalValidator) (stop bool, err error) {
		vals = append(vals, val)
		return false, nil
	})

	if err != nil {
		return nil, err
	}

	k.Logger().Debug("fetched all universal validators", "count", len(vals))
	return vals, nil
}

// GetValidatorsByStatus returns a list of validators filtered by status (ACTIVE, PENDING_JOIN, etc.).
func (k Keeper) GetValidatorsByStatus(ctx context.Context, status types.UVStatus) ([]types.UniversalValidator, error) {
	var vals []types.UniversalValidator

	err := k.UniversalValidatorSet.Walk(ctx, nil, func(addr sdk.ValAddress, val types.UniversalValidator) (stop bool, err error) {
		if val.LifecycleInfo.CurrentStatus == status {
			vals = append(vals, val)
		}
		return false, nil
	})

	if err != nil {
		return nil, err
	}

	k.Logger().Debug("fetched validators by status", "status", status.String(), "count", len(vals))
	return vals, nil
}

// GetEligibleVoters returns all validators that are eligible to vote on external transactions.
// Eligibility: validators with status ACTIVE or PENDING_JOIN.
func (k Keeper) GetEligibleVoters(ctx context.Context) ([]types.UniversalValidator, error) {
	var voters []types.UniversalValidator

	err := k.UniversalValidatorSet.Walk(ctx, nil, func(addr sdk.ValAddress, val types.UniversalValidator) (stop bool, err error) {
		switch val.LifecycleInfo.CurrentStatus {
		case types.UVStatus_UV_STATUS_ACTIVE, types.UVStatus_UV_STATUS_PENDING_JOIN:
			voters = append(voters, val)
		}
		return false, nil
	})

	if err != nil {
		return nil, err
	}

	k.Logger().Debug("fetched eligible voters", "count", len(voters))
	return voters, nil
}

// UpdateValidatorStatus updates the validator's lifecycle status.
// It appends a LifecycleEvent, validates legal transitions, and saves the updated record.
func (k Keeper) UpdateValidatorStatus(ctx context.Context, addr sdk.ValAddress, newStatus types.UVStatus) error {
	val, err := k.UniversalValidatorSet.Get(ctx, addr)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return fmt.Errorf("validator %s not found", addr)
		}
		return err
	}

	oldStatus := val.LifecycleInfo.CurrentStatus

	// Validate status transition
	if err := validateStatusTransition(oldStatus, newStatus); err != nil {
		k.Logger().Warn("invalid validator status transition",
			"validator", addr.String(),
			"old_status", oldStatus.String(),
			"new_status", newStatus.String(),
			"error", err.Error(),
		)
		return err
	}

	blockHeight := sdk.UnwrapSDKContext(ctx).BlockHeight()

	// Update lifecycle info
	event := types.LifecycleEvent{
		Status:      newStatus,
		BlockHeight: blockHeight,
	}
	val.LifecycleInfo.History = append(val.LifecycleInfo.History, &event)
	val.LifecycleInfo.CurrentStatus = newStatus

	// Save back to state
	if err := k.UniversalValidatorSet.Set(ctx, addr, val); err != nil {
		return err
	}

	k.Logger().Debug("validator status updated",
		"validator", addr.String(),
		"old_status", oldStatus.String(),
		"new_status", newStatus.String(),
		"block_height", blockHeight,
	)

	return nil
}

// validateStatusTransition ensures a validator can only move in a legal state order.
// only strict rule for two cases, pending join -> active & active -> pending_leave
// can see in future if a pending leave could be transitioned to pending_join
func validateStatusTransition(from, to types.UVStatus) error {
	switch to {
	// Comment out for new UpdateUniversalValidatorStatus
	// case types.UVStatus_UV_STATUS_ACTIVE:
	// 	if from != types.UVStatus_UV_STATUS_PENDING_JOIN {
	// 		return fmt.Errorf("invalid transition: can only become ACTIVE from PENDING_JOIN, got %s → %s", from, to)
	// 	}
	case types.UVStatus_UV_STATUS_PENDING_LEAVE:
		if from != types.UVStatus_UV_STATUS_ACTIVE {
			return fmt.Errorf("invalid transition: can only become PENDING_LEAVE from ACTIVE, got %s → %s", from, to)
		}
	}
	return nil
}

// GetUniversalValidator returns a single UniversalValidator by address.
func (k Keeper) GetUniversalValidator(
	ctx context.Context,
	addr sdk.ValAddress,
) (types.UniversalValidator, bool, error) {

	k.Logger().Debug("looking up universal validator", "validator", addr.String())

	val, err := k.UniversalValidatorSet.Get(ctx, addr)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			k.Logger().Debug("universal validator not found", "validator", addr.String())
			return types.UniversalValidator{}, false, nil
		}
		return types.UniversalValidator{}, false, err
	}

	k.Logger().Debug("universal validator found",
		"validator", addr.String(),
		"status", val.LifecycleInfo.CurrentStatus.String(),
	)

	return val, true, nil
}

// IsActiveUniversalValidator returns true if the given validator address is
// currently registered as an active universal validator.
func (k Keeper) IsActiveUniversalValidator(
	ctx context.Context,
	validatorOperatorAddr string,
) (bool, error) {
	valAddr, err := sdk.ValAddressFromBech32(validatorOperatorAddr)
	if err != nil {
		return false, err
	}

	exists, err := k.UniversalValidatorSet.Has(ctx, valAddr)
	if err != nil {
		return false, err
	}
	if !exists {
		return false, nil
	}

	uv, err := k.UniversalValidatorSet.Get(ctx, valAddr)
	if err != nil {
		return false, fmt.Errorf("failed to get universal validator: %w", err)
	}

	isActive := uv.LifecycleInfo.CurrentStatus == types.UVStatus_UV_STATUS_ACTIVE
	k.Logger().Debug("checked active universal validator status",
		"validator", validatorOperatorAddr,
		"is_active", isActive,
		"current_status", uv.LifecycleInfo.CurrentStatus.String(),
	)

	return isActive, nil
}
