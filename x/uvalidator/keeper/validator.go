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
	return voters, nil
}

// UpdateValidatorStatus updates the validator’s lifecycle status.
// It appends a LifecycleEvent, validates legal transitions, and saves the updated record.
func (k Keeper) UpdateValidatorStatus(ctx context.Context, addr sdk.ValAddress, newStatus types.UVStatus) error {
	val, err := k.UniversalValidatorSet.Get(ctx, addr)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return fmt.Errorf("validator %s not found", addr)
		}
		return err
	}

	// Validate status transition
	if err := validateStatusTransition(val.LifecycleInfo.CurrentStatus, newStatus); err != nil {
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
	return k.UniversalValidatorSet.Set(ctx, addr, val)
}

// validateStatusTransition ensures a validator can only move in a legal state order.
// only strict rule for two cases, pending join -> active & active -> pending_leave
// can see in future if a pending leave could be transitioned to pending_join
func validateStatusTransition(from, to types.UVStatus) error {
	switch to {
	case types.UVStatus_UV_STATUS_ACTIVE:
		if from != types.UVStatus_UV_STATUS_PENDING_JOIN {
			return fmt.Errorf("invalid transition: can only become ACTIVE from PENDING_JOIN, got %s → %s", from, to)
		}
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

	val, err := k.UniversalValidatorSet.Get(ctx, addr)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return types.UniversalValidator{}, false, nil
		}
		return types.UniversalValidator{}, false, err
	}

	return val, true, nil
}
