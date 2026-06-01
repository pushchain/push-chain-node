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
//
// Eligibility requires BOTH:
//   - UV lifecycle status is ACTIVE or PENDING_JOIN; AND
//   - the underlying Cosmos staking validator is bonded and not tombstoned.
//
// The staking-state filter prevents stranded UVs (still ACTIVE on paper but
// unbonded/jailed/tombstoned on the base chain) from inflating the ballot
// quorum denominator. Vote admission already rejects such signers, so without
// this filter the ballot threshold can become unreachable and finalization
// deadlocks.
func (k Keeper) GetEligibleVoters(ctx context.Context) ([]types.UniversalValidator, error) {
	var voters []types.UniversalValidator
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	err := k.UniversalValidatorSet.Walk(ctx, nil, func(addr sdk.ValAddress, val types.UniversalValidator) (stop bool, err error) {
		switch val.LifecycleInfo.CurrentStatus {
		case types.UVStatus_UV_STATUS_ACTIVE, types.UVStatus_UV_STATUS_PENDING_JOIN:
		default:
			return false, nil
		}

		sv, getErr := k.StakingKeeper.GetValidator(ctx, addr)
		if getErr != nil {
			// Validator removed from staking module, or some other read error:
			// treat as ineligible for this call rather than failing the whole
			// walk. This keeps quorum computable when one stranded entry would
			// otherwise crash the read path.
			k.Logger().Debug("eligible voter filter: staking GetValidator failed", "validator", addr.String(), "err", getErr)
			return false, nil
		}
		if !sv.IsBonded() {
			return false, nil
		}
		consAddr, caErr := sv.GetConsAddr()
		if caErr != nil {
			k.Logger().Debug("eligible voter filter: GetConsAddr failed", "validator", addr.String(), "err", caErr)
			return false, nil
		}
		if k.SlashingKeeper.IsTombstoned(sdkCtx, consAddr) {
			return false, nil
		}

		voters = append(voters, val)
		return false, nil
	})

	if err != nil {
		return nil, err
	}

	k.Logger().Debug("fetched eligible voters", "count", len(voters))
	return voters, nil
}

// UpdateValidatorStatus appends a lifecycle event and persists the new status.
// The reason is consulted by HandleBaseValidatorBonded to decide auto-revival.
func (k Keeper) UpdateValidatorStatus(ctx context.Context, addr sdk.ValAddress, newStatus types.UVStatus, reason types.TransitionReason) error {
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
		Reason:      reason,
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
		"reason", reason.String(),
		"block_height", blockHeight,
	)

	return nil
}

// HandleBaseValidatorUnbonding transitions the UV out of voting eligibility
// when the base validator begins unbonding. ACTIVE → PENDING_LEAVE,
// PENDING_JOIN → INACTIVE; other states no-op. Bypasses admin TSS guards
// since the base chain has already invalidated the validator. Records
// STAKING_HOOK reason for later auto-revival on re-bond. Errors are logged
// and swallowed so staking EndBlocker is never blocked.
func (k Keeper) HandleBaseValidatorUnbonding(ctx sdk.Context, valAddr sdk.ValAddress) {
	val, err := k.UniversalValidatorSet.Get(ctx, valAddr)
	if err != nil {
		return
	}

	oldStatus := val.LifecycleInfo.CurrentStatus
	var newStatus types.UVStatus
	switch oldStatus {
	case types.UVStatus_UV_STATUS_ACTIVE:
		newStatus = types.UVStatus_UV_STATUS_PENDING_LEAVE
	case types.UVStatus_UV_STATUS_PENDING_JOIN:
		newStatus = types.UVStatus_UV_STATUS_INACTIVE
	default:
		return
	}

	if err := k.UpdateValidatorStatus(ctx, valAddr, newStatus, types.TransitionReason_TRANSITION_REASON_STAKING_HOOK); err != nil {
		k.Logger().Error("staking hook: UV transition failed on base validator unbond",
			"validator", valAddr.String(),
			"old_status", oldStatus.String(),
			"new_status", newStatus.String(),
			"error", err,
		)
		return
	}

	k.Logger().Info("staking hook: UV transitioned due to base validator unbonding",
		"validator", valAddr.String(),
		"old_status", oldStatus.String(),
		"new_status", newStatus.String(),
	)

	if k.hooks != nil {
		k.hooks.AfterValidatorStatusChanged(ctx, valAddr, oldStatus, newStatus)
		k.hooks.AfterValidatorRemoved(ctx, valAddr)
	}
}

// HandleBaseValidatorBonded auto-revives a UV when the base validator returns
// to bonded state, but only if the latest lifecycle event was STAKING_HOOK-
// driven. PENDING_LEAVE → ACTIVE, INACTIVE → PENDING_JOIN; admin-driven
// removals stay put until operator reactivates. Errors are logged and swallowed.
func (k Keeper) HandleBaseValidatorBonded(ctx sdk.Context, valAddr sdk.ValAddress) {
	val, err := k.UniversalValidatorSet.Get(ctx, valAddr)
	if err != nil {
		return
	}

	oldStatus := val.LifecycleInfo.CurrentStatus
	var newStatus types.UVStatus
	switch oldStatus {
	case types.UVStatus_UV_STATUS_PENDING_LEAVE:
		newStatus = types.UVStatus_UV_STATUS_ACTIVE
	case types.UVStatus_UV_STATUS_INACTIVE:
		newStatus = types.UVStatus_UV_STATUS_PENDING_JOIN
	default:
		return
	}

	history := val.LifecycleInfo.History
	if len(history) == 0 || history[len(history)-1].Reason != types.TransitionReason_TRANSITION_REASON_STAKING_HOOK {
		k.Logger().Debug("staking hook (bonded): UV not auto-revivable (reason != STAKING_HOOK)",
			"validator", valAddr.String(),
			"current_status", oldStatus.String(),
		)
		return
	}

	if err := k.UpdateValidatorStatus(ctx, valAddr, newStatus, types.TransitionReason_TRANSITION_REASON_STAKING_HOOK); err != nil {
		k.Logger().Error("staking hook: UV revival failed on base validator bond",
			"validator", valAddr.String(),
			"old_status", oldStatus.String(),
			"new_status", newStatus.String(),
			"error", err,
		)
		return
	}

	k.Logger().Info("staking hook: UV auto-revived after base validator re-bonded",
		"validator", valAddr.String(),
		"old_status", oldStatus.String(),
		"new_status", newStatus.String(),
	)

	if k.hooks != nil {
		k.hooks.AfterValidatorStatusChanged(ctx, valAddr, oldStatus, newStatus)
		k.hooks.AfterValidatorAdded(ctx, valAddr)
	}
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
