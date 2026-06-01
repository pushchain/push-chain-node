package integrationtest

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/app"
	utils "github.com/pushchain/push-chain-node/test/utils"
	uvalidatortypes "github.com/pushchain/push-chain-node/x/uvalidator/types"
)

// F-2026-16991 #1 regression suite: staking-hook-driven UV lifecycle
// transitions + reason-based auto-revival.
//
// Coverage map (see plan doc):
//   A. UpdateValidatorStatus records reason             — TestUpdateValidatorStatus_RecordsReason
//   B. HandleBaseValidatorUnbonding (unbond direction)  — TestHandleBaseValidatorUnbonding_*
//   C. HandleBaseValidatorBonded   (revival direction)  — TestHandleBaseValidatorBonded_*
//   D. Admin callers pass ADMIN reason                  — TestAdminCallersRecordAdminReason
//   E. TSS finalization preserves prior reason          — TestTssFinalization_PreservesReason
//   F. End-to-end auto-revival                          — TestEndToEnd_AutoRevival_*
//   H. StakingHooks interface wiring                    — TestStakingHooks_Interface_*

// latestEvent returns the most recent LifecycleEvent on a UV. Fails the test
// if the UV has no history.
func latestEvent(t *testing.T, chainApp *app.ChainApp, ctx sdk.Context, addr sdk.ValAddress) *uvalidatortypes.LifecycleEvent {
	t.Helper()
	uv, err := chainApp.UvalidatorKeeper.UniversalValidatorSet.Get(ctx, addr)
	require.NoError(t, err)
	require.NotEmpty(t, uv.LifecycleInfo.History, "expected UV %s to have lifecycle history", addr)
	return uv.LifecycleInfo.History[len(uv.LifecycleInfo.History)-1]
}

// currentStatus returns the UV's current lifecycle status.
func currentStatus(t *testing.T, chainApp *app.ChainApp, ctx sdk.Context, addr sdk.ValAddress) uvalidatortypes.UVStatus {
	t.Helper()
	uv, err := chainApp.UvalidatorKeeper.UniversalValidatorSet.Get(ctx, addr)
	require.NoError(t, err)
	return uv.LifecycleInfo.CurrentStatus
}

// ============================================================================
// A. UpdateValidatorStatus records reason
// ============================================================================

func TestUpdateValidatorStatus_RecordsReason(t *testing.T) {
	t.Run("records ADMIN reason when passed", func(t *testing.T) {
		chainApp, ctx, validators := setupQueryTest(t, 1)
		valAddr, _ := sdk.ValAddressFromBech32(validators[0].OperatorAddress)
		setUVStatus(t, chainApp, ctx, validators[0], uvalidatortypes.UVStatus_UV_STATUS_ACTIVE)

		err := chainApp.UvalidatorKeeper.UpdateValidatorStatus(ctx, valAddr,
			uvalidatortypes.UVStatus_UV_STATUS_PENDING_LEAVE,
			uvalidatortypes.TransitionReason_TRANSITION_REASON_ADMIN)
		require.NoError(t, err)

		evt := latestEvent(t, chainApp, ctx, valAddr)
		require.Equal(t, uvalidatortypes.UVStatus_UV_STATUS_PENDING_LEAVE, evt.Status)
		require.Equal(t, uvalidatortypes.TransitionReason_TRANSITION_REASON_ADMIN, evt.Reason)
	})

	t.Run("records STAKING_HOOK reason when passed", func(t *testing.T) {
		chainApp, ctx, validators := setupQueryTest(t, 1)
		valAddr, _ := sdk.ValAddressFromBech32(validators[0].OperatorAddress)
		setUVStatus(t, chainApp, ctx, validators[0], uvalidatortypes.UVStatus_UV_STATUS_ACTIVE)

		err := chainApp.UvalidatorKeeper.UpdateValidatorStatus(ctx, valAddr,
			uvalidatortypes.UVStatus_UV_STATUS_PENDING_LEAVE,
			uvalidatortypes.TransitionReason_TRANSITION_REASON_STAKING_HOOK)
		require.NoError(t, err)

		evt := latestEvent(t, chainApp, ctx, valAddr)
		require.Equal(t, uvalidatortypes.TransitionReason_TRANSITION_REASON_STAKING_HOOK, evt.Reason)
	})

	t.Run("records UNSPECIFIED reason when passed", func(t *testing.T) {
		chainApp, ctx, validators := setupQueryTest(t, 1)
		valAddr, _ := sdk.ValAddressFromBech32(validators[0].OperatorAddress)
		setUVStatus(t, chainApp, ctx, validators[0], uvalidatortypes.UVStatus_UV_STATUS_ACTIVE)

		err := chainApp.UvalidatorKeeper.UpdateValidatorStatus(ctx, valAddr,
			uvalidatortypes.UVStatus_UV_STATUS_PENDING_LEAVE,
			uvalidatortypes.TransitionReason_TRANSITION_REASON_UNSPECIFIED)
		require.NoError(t, err)

		evt := latestEvent(t, chainApp, ctx, valAddr)
		require.Equal(t, uvalidatortypes.TransitionReason_TRANSITION_REASON_UNSPECIFIED, evt.Reason)
	})
}

// ============================================================================
// B. HandleBaseValidatorUnbonding
// ============================================================================

func TestHandleBaseValidatorUnbonding(t *testing.T) {
	t.Run("ACTIVE → PENDING_LEAVE with STAKING_HOOK reason", func(t *testing.T) {
		chainApp, ctx, validators := setupQueryTest(t, 1)
		valAddr, _ := sdk.ValAddressFromBech32(validators[0].OperatorAddress)
		setUVStatus(t, chainApp, ctx, validators[0], uvalidatortypes.UVStatus_UV_STATUS_ACTIVE)

		chainApp.UvalidatorKeeper.HandleBaseValidatorUnbonding(ctx, valAddr)

		require.Equal(t, uvalidatortypes.UVStatus_UV_STATUS_PENDING_LEAVE,
			currentStatus(t, chainApp, ctx, valAddr))

		evt := latestEvent(t, chainApp, ctx, valAddr)
		require.Equal(t, uvalidatortypes.UVStatus_UV_STATUS_PENDING_LEAVE, evt.Status)
		require.Equal(t, uvalidatortypes.TransitionReason_TRANSITION_REASON_STAKING_HOOK, evt.Reason)
	})

	t.Run("PENDING_JOIN → INACTIVE with STAKING_HOOK reason", func(t *testing.T) {
		chainApp, ctx, validators := setupQueryTest(t, 1)
		valAddr, _ := sdk.ValAddressFromBech32(validators[0].OperatorAddress)
		// setupQueryTest already registers as PENDING_JOIN — leave it.

		chainApp.UvalidatorKeeper.HandleBaseValidatorUnbonding(ctx, valAddr)

		require.Equal(t, uvalidatortypes.UVStatus_UV_STATUS_INACTIVE,
			currentStatus(t, chainApp, ctx, valAddr))

		evt := latestEvent(t, chainApp, ctx, valAddr)
		require.Equal(t, uvalidatortypes.UVStatus_UV_STATUS_INACTIVE, evt.Status)
		require.Equal(t, uvalidatortypes.TransitionReason_TRANSITION_REASON_STAKING_HOOK, evt.Reason)
	})

	t.Run("PENDING_LEAVE → no-op", func(t *testing.T) {
		chainApp, ctx, validators := setupQueryTest(t, 1)
		valAddr, _ := sdk.ValAddressFromBech32(validators[0].OperatorAddress)
		setUVStatus(t, chainApp, ctx, validators[0], uvalidatortypes.UVStatus_UV_STATUS_PENDING_LEAVE)

		before, _ := chainApp.UvalidatorKeeper.UniversalValidatorSet.Get(ctx, valAddr)
		chainApp.UvalidatorKeeper.HandleBaseValidatorUnbonding(ctx, valAddr)
		after, _ := chainApp.UvalidatorKeeper.UniversalValidatorSet.Get(ctx, valAddr)

		require.Equal(t, uvalidatortypes.UVStatus_UV_STATUS_PENDING_LEAVE, after.LifecycleInfo.CurrentStatus,
			"PENDING_LEAVE should stay PENDING_LEAVE")
		require.Equal(t, len(before.LifecycleInfo.History), len(after.LifecycleInfo.History),
			"no new lifecycle event should be appended")
	})

	t.Run("INACTIVE → no-op", func(t *testing.T) {
		chainApp, ctx, validators := setupQueryTest(t, 1)
		valAddr, _ := sdk.ValAddressFromBech32(validators[0].OperatorAddress)
		setUVStatus(t, chainApp, ctx, validators[0], uvalidatortypes.UVStatus_UV_STATUS_INACTIVE)

		before, _ := chainApp.UvalidatorKeeper.UniversalValidatorSet.Get(ctx, valAddr)
		chainApp.UvalidatorKeeper.HandleBaseValidatorUnbonding(ctx, valAddr)
		after, _ := chainApp.UvalidatorKeeper.UniversalValidatorSet.Get(ctx, valAddr)

		require.Equal(t, uvalidatortypes.UVStatus_UV_STATUS_INACTIVE, after.LifecycleInfo.CurrentStatus)
		require.Equal(t, len(before.LifecycleInfo.History), len(after.LifecycleInfo.History))
	})

	t.Run("non-UV validator → no-op (no panic)", func(t *testing.T) {
		chainApp, ctx, _, validators := utils.SetAppWithMultipleValidators(t, 2)
		// Register only validators[0] as a UV; validators[1] is a staking validator only.
		registerUV(t, chainApp, ctx, validators[0], 0)
		valAddr, _ := sdk.ValAddressFromBech32(validators[1].OperatorAddress)

		require.NotPanics(t, func() {
			chainApp.UvalidatorKeeper.HandleBaseValidatorUnbonding(ctx, valAddr)
		})

		_, err := chainApp.UvalidatorKeeper.UniversalValidatorSet.Get(ctx, valAddr)
		require.Error(t, err, "non-UV validator should remain absent from UV set")
	})
}

// ============================================================================
// C. HandleBaseValidatorBonded — auto-revival logic
// ============================================================================

func TestHandleBaseValidatorBonded(t *testing.T) {
	t.Run("PENDING_LEAVE + STAKING_HOOK reason → revives to ACTIVE", func(t *testing.T) {
		chainApp, ctx, validators := setupQueryTest(t, 1)
		valAddr, _ := sdk.ValAddressFromBech32(validators[0].OperatorAddress)

		// Simulate prior hook-driven transition to PENDING_LEAVE.
		setUVStatus(t, chainApp, ctx, validators[0], uvalidatortypes.UVStatus_UV_STATUS_ACTIVE)
		chainApp.UvalidatorKeeper.HandleBaseValidatorUnbonding(ctx, valAddr)
		require.Equal(t, uvalidatortypes.UVStatus_UV_STATUS_PENDING_LEAVE,
			currentStatus(t, chainApp, ctx, valAddr))

		// Now base validator re-bonds → revival.
		chainApp.UvalidatorKeeper.HandleBaseValidatorBonded(ctx, valAddr)

		require.Equal(t, uvalidatortypes.UVStatus_UV_STATUS_ACTIVE,
			currentStatus(t, chainApp, ctx, valAddr))
		evt := latestEvent(t, chainApp, ctx, valAddr)
		require.Equal(t, uvalidatortypes.TransitionReason_TRANSITION_REASON_STAKING_HOOK, evt.Reason)
	})

	t.Run("PENDING_LEAVE + ADMIN reason → no-op", func(t *testing.T) {
		chainApp, ctx, validators := setupQueryTest(t, 1)
		valAddr, _ := sdk.ValAddressFromBech32(validators[0].OperatorAddress)
		setUVStatus(t, chainApp, ctx, validators[0], uvalidatortypes.UVStatus_UV_STATUS_ACTIVE)

		// Simulate admin-driven transition to PENDING_LEAVE.
		require.NoError(t, chainApp.UvalidatorKeeper.UpdateValidatorStatus(ctx, valAddr,
			uvalidatortypes.UVStatus_UV_STATUS_PENDING_LEAVE,
			uvalidatortypes.TransitionReason_TRANSITION_REASON_ADMIN))

		// Base validator re-bonds — must NOT auto-revive.
		chainApp.UvalidatorKeeper.HandleBaseValidatorBonded(ctx, valAddr)

		require.Equal(t, uvalidatortypes.UVStatus_UV_STATUS_PENDING_LEAVE,
			currentStatus(t, chainApp, ctx, valAddr),
			"admin-driven removal should not auto-revive")
	})

	t.Run("PENDING_LEAVE + UNSPECIFIED reason → no-op (conservative)", func(t *testing.T) {
		chainApp, ctx, validators := setupQueryTest(t, 1)
		valAddr, _ := sdk.ValAddressFromBech32(validators[0].OperatorAddress)
		setUVStatus(t, chainApp, ctx, validators[0], uvalidatortypes.UVStatus_UV_STATUS_ACTIVE)

		require.NoError(t, chainApp.UvalidatorKeeper.UpdateValidatorStatus(ctx, valAddr,
			uvalidatortypes.UVStatus_UV_STATUS_PENDING_LEAVE,
			uvalidatortypes.TransitionReason_TRANSITION_REASON_UNSPECIFIED))

		chainApp.UvalidatorKeeper.HandleBaseValidatorBonded(ctx, valAddr)

		require.Equal(t, uvalidatortypes.UVStatus_UV_STATUS_PENDING_LEAVE,
			currentStatus(t, chainApp, ctx, valAddr),
			"UNSPECIFIED reason should NOT auto-revive (conservative for legacy data)")
	})

	t.Run("INACTIVE + STAKING_HOOK reason → revives to PENDING_JOIN", func(t *testing.T) {
		chainApp, ctx, validators := setupQueryTest(t, 1)
		valAddr, _ := sdk.ValAddressFromBech32(validators[0].OperatorAddress)
		// UV starts in PENDING_JOIN (from setupQueryTest), simulate hook-driven unbond.
		chainApp.UvalidatorKeeper.HandleBaseValidatorUnbonding(ctx, valAddr)
		require.Equal(t, uvalidatortypes.UVStatus_UV_STATUS_INACTIVE,
			currentStatus(t, chainApp, ctx, valAddr))

		chainApp.UvalidatorKeeper.HandleBaseValidatorBonded(ctx, valAddr)

		require.Equal(t, uvalidatortypes.UVStatus_UV_STATUS_PENDING_JOIN,
			currentStatus(t, chainApp, ctx, valAddr))
		evt := latestEvent(t, chainApp, ctx, valAddr)
		require.Equal(t, uvalidatortypes.TransitionReason_TRANSITION_REASON_STAKING_HOOK, evt.Reason)
	})

	t.Run("INACTIVE + ADMIN reason → no-op", func(t *testing.T) {
		chainApp, ctx, validators := setupQueryTest(t, 1)
		valAddr, _ := sdk.ValAddressFromBech32(validators[0].OperatorAddress)
		setUVStatus(t, chainApp, ctx, validators[0], uvalidatortypes.UVStatus_UV_STATUS_PENDING_JOIN)

		require.NoError(t, chainApp.UvalidatorKeeper.UpdateValidatorStatus(ctx, valAddr,
			uvalidatortypes.UVStatus_UV_STATUS_INACTIVE,
			uvalidatortypes.TransitionReason_TRANSITION_REASON_ADMIN))

		chainApp.UvalidatorKeeper.HandleBaseValidatorBonded(ctx, valAddr)

		require.Equal(t, uvalidatortypes.UVStatus_UV_STATUS_INACTIVE,
			currentStatus(t, chainApp, ctx, valAddr))
	})

	t.Run("ACTIVE → no-op (already eligible)", func(t *testing.T) {
		chainApp, ctx, validators := setupQueryTest(t, 1)
		valAddr, _ := sdk.ValAddressFromBech32(validators[0].OperatorAddress)
		setUVStatus(t, chainApp, ctx, validators[0], uvalidatortypes.UVStatus_UV_STATUS_ACTIVE)

		before, _ := chainApp.UvalidatorKeeper.UniversalValidatorSet.Get(ctx, valAddr)
		chainApp.UvalidatorKeeper.HandleBaseValidatorBonded(ctx, valAddr)
		after, _ := chainApp.UvalidatorKeeper.UniversalValidatorSet.Get(ctx, valAddr)

		require.Equal(t, uvalidatortypes.UVStatus_UV_STATUS_ACTIVE, after.LifecycleInfo.CurrentStatus)
		require.Equal(t, len(before.LifecycleInfo.History), len(after.LifecycleInfo.History))
	})

	t.Run("PENDING_JOIN → no-op (already eligible)", func(t *testing.T) {
		chainApp, ctx, validators := setupQueryTest(t, 1)
		valAddr, _ := sdk.ValAddressFromBech32(validators[0].OperatorAddress)
		// UV starts in PENDING_JOIN from setupQueryTest.

		before, _ := chainApp.UvalidatorKeeper.UniversalValidatorSet.Get(ctx, valAddr)
		chainApp.UvalidatorKeeper.HandleBaseValidatorBonded(ctx, valAddr)
		after, _ := chainApp.UvalidatorKeeper.UniversalValidatorSet.Get(ctx, valAddr)

		require.Equal(t, uvalidatortypes.UVStatus_UV_STATUS_PENDING_JOIN, after.LifecycleInfo.CurrentStatus)
		require.Equal(t, len(before.LifecycleInfo.History), len(after.LifecycleInfo.History))
	})

	t.Run("non-UV validator → no-op (no panic)", func(t *testing.T) {
		chainApp, ctx, _, validators := utils.SetAppWithMultipleValidators(t, 2)
		registerUV(t, chainApp, ctx, validators[0], 0)
		valAddr, _ := sdk.ValAddressFromBech32(validators[1].OperatorAddress)

		require.NotPanics(t, func() {
			chainApp.UvalidatorKeeper.HandleBaseValidatorBonded(ctx, valAddr)
		})
	})
}

// ============================================================================
// D. Admin callers pass ADMIN reason
// ============================================================================

func TestAdminCallersRecordAdminReason(t *testing.T) {
	t.Run("RemoveUniversalValidator (ACTIVE → PENDING_LEAVE) records ADMIN", func(t *testing.T) {
		chainApp, ctx, validators := setupQueryTest(t, 1)
		valAddr, _ := sdk.ValAddressFromBech32(validators[0].OperatorAddress)
		setUVStatus(t, chainApp, ctx, validators[0], uvalidatortypes.UVStatus_UV_STATUS_ACTIVE)

		require.NoError(t, chainApp.UvalidatorKeeper.RemoveUniversalValidator(ctx, valAddr.String()))

		evt := latestEvent(t, chainApp, ctx, valAddr)
		require.Equal(t, uvalidatortypes.UVStatus_UV_STATUS_PENDING_LEAVE, evt.Status)
		require.Equal(t, uvalidatortypes.TransitionReason_TRANSITION_REASON_ADMIN, evt.Reason)
	})

	t.Run("RemoveUniversalValidator (PENDING_JOIN → INACTIVE) records ADMIN", func(t *testing.T) {
		chainApp, ctx, validators := setupQueryTest(t, 1)
		valAddr, _ := sdk.ValAddressFromBech32(validators[0].OperatorAddress)
		// PENDING_JOIN from setupQueryTest.

		// Ensure no TSS process so PENDING_JOIN→INACTIVE branch is reached.
		require.NoError(t, chainApp.UtssKeeper.CurrentTssProcess.Remove(ctx))

		require.NoError(t, chainApp.UvalidatorKeeper.RemoveUniversalValidator(ctx, valAddr.String()))

		evt := latestEvent(t, chainApp, ctx, valAddr)
		require.Equal(t, uvalidatortypes.UVStatus_UV_STATUS_INACTIVE, evt.Status)
		require.Equal(t, uvalidatortypes.TransitionReason_TRANSITION_REASON_ADMIN, evt.Reason)
	})

	t.Run("UpdateUniversalValidatorStatus (PENDING_LEAVE → ACTIVE) records ADMIN", func(t *testing.T) {
		chainApp, ctx, validators := setupQueryTest(t, 1)
		valAddr, _ := sdk.ValAddressFromBech32(validators[0].OperatorAddress)
		setUVStatus(t, chainApp, ctx, validators[0], uvalidatortypes.UVStatus_UV_STATUS_PENDING_LEAVE)

		require.NoError(t, chainApp.UvalidatorKeeper.UpdateUniversalValidatorStatus(ctx, valAddr.String(),
			uvalidatortypes.UVStatus_UV_STATUS_ACTIVE))

		evt := latestEvent(t, chainApp, ctx, valAddr)
		require.Equal(t, uvalidatortypes.UVStatus_UV_STATUS_ACTIVE, evt.Status)
		require.Equal(t, uvalidatortypes.TransitionReason_TRANSITION_REASON_ADMIN, evt.Reason)
	})
}

// ============================================================================
// H. StakingHooks interface wiring
// ============================================================================

func TestStakingHooks_Interface(t *testing.T) {
	t.Run("AfterValidatorBeginUnbonding delegates to HandleBaseValidatorUnbonding", func(t *testing.T) {
		chainApp, ctx, validators := setupQueryTest(t, 1)
		valAddr, _ := sdk.ValAddressFromBech32(validators[0].OperatorAddress)
		setUVStatus(t, chainApp, ctx, validators[0], uvalidatortypes.UVStatus_UV_STATUS_ACTIVE)

		// Fire the staking hook directly.
		consAddr, err := validators[0].GetConsAddr()
		require.NoError(t, err)
		require.NoError(t, chainApp.UvalidatorKeeper.StakingHooks().AfterValidatorBeginUnbonding(ctx, consAddr, valAddr))

		require.Equal(t, uvalidatortypes.UVStatus_UV_STATUS_PENDING_LEAVE,
			currentStatus(t, chainApp, ctx, valAddr))
		evt := latestEvent(t, chainApp, ctx, valAddr)
		require.Equal(t, uvalidatortypes.TransitionReason_TRANSITION_REASON_STAKING_HOOK, evt.Reason)
	})

	t.Run("AfterValidatorBonded delegates to HandleBaseValidatorBonded", func(t *testing.T) {
		chainApp, ctx, validators := setupQueryTest(t, 1)
		valAddr, _ := sdk.ValAddressFromBech32(validators[0].OperatorAddress)
		setUVStatus(t, chainApp, ctx, validators[0], uvalidatortypes.UVStatus_UV_STATUS_ACTIVE)

		// Set up PENDING_LEAVE/STAKING_HOOK via the hook.
		consAddr, _ := validators[0].GetConsAddr()
		require.NoError(t, chainApp.UvalidatorKeeper.StakingHooks().AfterValidatorBeginUnbonding(ctx, consAddr, valAddr))
		require.Equal(t, uvalidatortypes.UVStatus_UV_STATUS_PENDING_LEAVE,
			currentStatus(t, chainApp, ctx, valAddr))

		// Now fire the bond hook → should revive.
		require.NoError(t, chainApp.UvalidatorKeeper.StakingHooks().AfterValidatorBonded(ctx, consAddr, valAddr))

		require.Equal(t, uvalidatortypes.UVStatus_UV_STATUS_ACTIVE,
			currentStatus(t, chainApp, ctx, valAddr))
	})

	t.Run("other hooks are no-op stubs (don't error)", func(t *testing.T) {
		chainApp, ctx, validators := setupQueryTest(t, 1)
		valAddr, _ := sdk.ValAddressFromBech32(validators[0].OperatorAddress)
		consAddr, _ := validators[0].GetConsAddr()
		accAddr := sdk.AccAddress(valAddr)

		h := chainApp.UvalidatorKeeper.StakingHooks()
		require.NoError(t, h.AfterValidatorCreated(ctx, valAddr))
		require.NoError(t, h.BeforeValidatorModified(ctx, valAddr))
		require.NoError(t, h.AfterValidatorRemoved(ctx, consAddr, valAddr))
		require.NoError(t, h.BeforeDelegationCreated(ctx, accAddr, valAddr))
		require.NoError(t, h.BeforeDelegationSharesModified(ctx, accAddr, valAddr))
		require.NoError(t, h.BeforeDelegationRemoved(ctx, accAddr, valAddr))
		require.NoError(t, h.AfterDelegationModified(ctx, accAddr, valAddr))
		require.NoError(t, h.AfterUnbondingInitiated(ctx, 0))
	})
}

// ============================================================================
// F. End-to-end auto-revival flow
// ============================================================================

func TestEndToEnd_AutoRevival_HookDrivenRoundtrip(t *testing.T) {
	// Validator unbonds (hook fires) → ACTIVE → PENDING_LEAVE/STAKING_HOOK
	// Validator re-bonds → PENDING_LEAVE → ACTIVE/STAKING_HOOK.
	chainApp, ctx, validators := setupQueryTest(t, 1)
	valAddr, _ := sdk.ValAddressFromBech32(validators[0].OperatorAddress)
	setUVStatus(t, chainApp, ctx, validators[0], uvalidatortypes.UVStatus_UV_STATUS_ACTIVE)
	consAddr, _ := validators[0].GetConsAddr()

	h := chainApp.UvalidatorKeeper.StakingHooks()

	// Step 1: validator unbonds.
	require.NoError(t, h.AfterValidatorBeginUnbonding(ctx, consAddr, valAddr))
	require.Equal(t, uvalidatortypes.UVStatus_UV_STATUS_PENDING_LEAVE,
		currentStatus(t, chainApp, ctx, valAddr))

	// Step 2: validator re-bonds.
	require.NoError(t, h.AfterValidatorBonded(ctx, consAddr, valAddr))
	require.Equal(t, uvalidatortypes.UVStatus_UV_STATUS_ACTIVE,
		currentStatus(t, chainApp, ctx, valAddr),
		"hook-driven removal must auto-revive on re-bond")
}

// TestStakingHook_ConcurrentUnbondings exercises the stress case the impact
// analysis flagged: multiple validators going through AfterValidatorBeginUnbonding
// in the same block. The hook chain triggers utss reshare each time; we verify
// each UV correctly transitions and the chain doesn't error out partway.
func TestStakingHook_ConcurrentUnbondings(t *testing.T) {
	chainApp, ctx, validators := setupQueryTest(t, 4)
	for _, v := range validators {
		setUVStatus(t, chainApp, ctx, v, uvalidatortypes.UVStatus_UV_STATUS_ACTIVE)
	}

	h := chainApp.UvalidatorKeeper.StakingHooks()

	// Fire hook for 3 of 4 validators back-to-back (simulating same-block events).
	for i := 0; i < 3; i++ {
		valAddr, _ := sdk.ValAddressFromBech32(validators[i].OperatorAddress)
		consAddr, _ := validators[i].GetConsAddr()
		require.NoError(t, h.AfterValidatorBeginUnbonding(ctx, consAddr, valAddr),
			"hook %d must succeed without erroring the staking EndBlocker", i)
	}

	// Verify all 3 transitioned to PENDING_LEAVE with STAKING_HOOK reason.
	for i := 0; i < 3; i++ {
		valAddr, _ := sdk.ValAddressFromBech32(validators[i].OperatorAddress)
		require.Equal(t, uvalidatortypes.UVStatus_UV_STATUS_PENDING_LEAVE,
			currentStatus(t, chainApp, ctx, valAddr),
			"validator %d should be PENDING_LEAVE", i)
		evt := latestEvent(t, chainApp, ctx, valAddr)
		require.Equal(t, uvalidatortypes.TransitionReason_TRANSITION_REASON_STAKING_HOOK, evt.Reason)
	}

	// 4th validator remains untouched.
	valAddr3, _ := sdk.ValAddressFromBech32(validators[3].OperatorAddress)
	require.Equal(t, uvalidatortypes.UVStatus_UV_STATUS_ACTIVE,
		currentStatus(t, chainApp, ctx, valAddr3),
		"untouched validator must remain ACTIVE")
}

// TestStakingHook_EligibleDropsBelowQuorum tests the edge case where firing
// the hook leaves only 1 eligible validator. utss's downstream hook attempts
// to start a new TSS process but bails ("TSS not possible") when count < 2.
// Verifies the UV transition itself still succeeds despite the TSS-side bail.
func TestStakingHook_EligibleDropsBelowQuorum(t *testing.T) {
	chainApp, ctx, validators := setupQueryTest(t, 2)
	for _, v := range validators {
		setUVStatus(t, chainApp, ctx, v, uvalidatortypes.UVStatus_UV_STATUS_ACTIVE)
	}

	h := chainApp.UvalidatorKeeper.StakingHooks()
	valAddr0, _ := sdk.ValAddressFromBech32(validators[0].OperatorAddress)
	consAddr0, _ := validators[0].GetConsAddr()

	// Fire hook on validator 0 — only validator 1 remains eligible (count=1).
	require.NoError(t, h.AfterValidatorBeginUnbonding(ctx, consAddr0, valAddr0),
		"hook must not error even when TSS quorum becomes impossible")

	// Validator 0 transitioned correctly.
	require.Equal(t, uvalidatortypes.UVStatus_UV_STATUS_PENDING_LEAVE,
		currentStatus(t, chainApp, ctx, valAddr0))
	// Validator 1 unchanged.
	valAddr1, _ := sdk.ValAddressFromBech32(validators[1].OperatorAddress)
	require.Equal(t, uvalidatortypes.UVStatus_UV_STATUS_ACTIVE,
		currentStatus(t, chainApp, ctx, valAddr1))
}

func TestEndToEnd_AutoRevival_AdminRemovalNotRevived(t *testing.T) {
	// Admin removes UV (reason=ADMIN) → validator coincidentally unbonds and
	// re-bonds via the hook path → UV stays PENDING_LEAVE (admin intent preserved).
	chainApp, ctx, validators := setupQueryTest(t, 1)
	valAddr, _ := sdk.ValAddressFromBech32(validators[0].OperatorAddress)
	setUVStatus(t, chainApp, ctx, validators[0], uvalidatortypes.UVStatus_UV_STATUS_ACTIVE)
	consAddr, _ := validators[0].GetConsAddr()

	// Admin removal.
	require.NoError(t, chainApp.UvalidatorKeeper.RemoveUniversalValidator(ctx, valAddr.String()))
	require.Equal(t, uvalidatortypes.UVStatus_UV_STATUS_PENDING_LEAVE,
		currentStatus(t, chainApp, ctx, valAddr))
	require.Equal(t, uvalidatortypes.TransitionReason_TRANSITION_REASON_ADMIN,
		latestEvent(t, chainApp, ctx, valAddr).Reason)

	// Hook fires for re-bond.
	require.NoError(t, chainApp.UvalidatorKeeper.StakingHooks().AfterValidatorBonded(ctx, consAddr, valAddr))

	require.Equal(t, uvalidatortypes.UVStatus_UV_STATUS_PENDING_LEAVE,
		currentStatus(t, chainApp, ctx, valAddr),
		"admin removal must NOT be overridden by hook-driven re-bond")
}

