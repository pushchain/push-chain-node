package integrationtest

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/app"
	utsstypes "github.com/pushchain/push-chain-node/x/utss/types"
	uvalidatortypes "github.com/pushchain/push-chain-node/x/uvalidator/types"
)

// mustValAddr converts a bech32 operator address to ValAddress, failing the test on error.
func mustValAddr(t *testing.T, bech32 string) sdk.ValAddress {
	t.Helper()
	addr, err := sdk.ValAddressFromBech32(bech32)
	require.NoError(t, err)
	return addr
}

// runTssReshareExcluding initiates a QUORUM_CHANGE TSS process (which excludes
// PENDING_LEAVE validators from participants) and votes it through to
// finalization. After finalization, any PENDING_LEAVE UV not in the new
// participant set will have been moved to INACTIVE by the TSS code, with the
// prior reason preserved (the F-2026-16991 #1 propagation behavior).
func runTssReshareExcluding(t *testing.T, app *app.ChainApp, ctx sdk.Context, excluded string) {
	t.Helper()
	require.NoError(t, app.UtssKeeper.InitiateTssKeyProcess(ctx, utsstypes.TssProcessType_TSS_PROCESS_QUORUM_CHANGE))
	process, err := app.UtssKeeper.CurrentTssProcess.Get(ctx)
	require.NoError(t, err)

	// Confirm excluded validator is NOT in the participant set.
	for _, p := range process.Participants {
		require.NotEqual(t, excluded, p,
			"runTssReshareExcluding expects %s to NOT be in TSS participants", excluded)
	}

	// Vote with each participant until finalized.
	finalizeAutoInitiatedTssProcess(t, app, ctx, "pubkey-reshare", "Key-id-reshare")
}

// TestInitiateTssKeyProcess_ForceExpiry_MarksTssEventExpired verifies that
// when InitiateTssKeyProcess force-expires an in-flight process, the
// corresponding TssEvent is updated from ACTIVE → EXPIRED and dropped from
// the PendingTssEvents index. Prior to F-2026-16991 #1, the TssEvent was
// left in ACTIVE status forever even though its underlying process was dead.
func TestInitiateTssKeyProcess_ForceExpiry_MarksTssEventExpired(t *testing.T) {
	app, ctx, _ := setupTssKeyProcessTest(t, 2)

	// Process A: pending.
	require.NoError(t, app.UtssKeeper.InitiateTssKeyProcess(ctx, utsstypes.TssProcessType_TSS_PROCESS_KEYGEN))
	procA, err := app.UtssKeeper.CurrentTssProcess.Get(ctx)
	require.NoError(t, err)

	// Lookup the event id indexed under procA's id and confirm it's currently ACTIVE.
	eventIdA, err := app.UtssKeeper.PendingTssEvents.Get(ctx, procA.Id)
	require.NoError(t, err, "process A must be in PendingTssEvents pre-force-expiry")
	evtA, err := app.UtssKeeper.TssEvents.Get(ctx, eventIdA)
	require.NoError(t, err)
	require.Equal(t, utsstypes.TssEventStatus_TSS_EVENT_ACTIVE, evtA.Status,
		"process A's event must be ACTIVE before the next InitiateTssKeyProcess")

	// Process B: triggers force-expiry of A.
	require.NoError(t, app.UtssKeeper.InitiateTssKeyProcess(ctx, utsstypes.TssProcessType_TSS_PROCESS_KEYGEN))
	procB, err := app.UtssKeeper.CurrentTssProcess.Get(ctx)
	require.NoError(t, err)
	require.NotEqual(t, procA.Id, procB.Id)

	// Process A's event must now be EXPIRED.
	evtA, err = app.UtssKeeper.TssEvents.Get(ctx, eventIdA)
	require.NoError(t, err)
	require.Equal(t, utsstypes.TssEventStatus_TSS_EVENT_EXPIRED, evtA.Status,
		"force-expired process's event must be marked EXPIRED")

	// Process A must be dropped from the pending index.
	_, err = app.UtssKeeper.PendingTssEvents.Get(ctx, procA.Id)
	require.Error(t, err, "force-expired process must be removed from PendingTssEvents")

	// Process B's event is ACTIVE and indexed as pending.
	eventIdB, err := app.UtssKeeper.PendingTssEvents.Get(ctx, procB.Id)
	require.NoError(t, err)
	evtB, err := app.UtssKeeper.TssEvents.Get(ctx, eventIdB)
	require.NoError(t, err)
	require.Equal(t, utsstypes.TssEventStatus_TSS_EVENT_ACTIVE, evtB.Status)
}

// TestTssFinalization_PreservesReason verifies that when TSS finalization
// transitions a PENDING_LEAVE UV to INACTIVE, the prior reason is propagated.
// This is load-bearing for HandleBaseValidatorBonded's auto-revival logic —
// without this, the original removal cause (admin vs staking-hook) would be
// lost at the finalization step.
func TestTssFinalization_PreservesReason(t *testing.T) {
	t.Run("PENDING_LEAVE with STAKING_HOOK → INACTIVE preserves STAKING_HOOK", func(t *testing.T) {
		app, ctx, validators := setupTssKeyProcessTest(t, 3)
		valAddr0 := mustValAddr(t, validators[0])

		// Move val[0] to PENDING_LEAVE with STAKING_HOOK reason (simulating hook-driven removal).
		require.NoError(t, app.UvalidatorKeeper.UpdateValidatorStatus(ctx, valAddr0,
			uvalidatortypes.UVStatus_UV_STATUS_PENDING_LEAVE,
			uvalidatortypes.TransitionReason_TRANSITION_REASON_STAKING_HOOK))

		// Run a TSS reshare that does NOT include val[0]. After finalization,
		// val[0] should move PENDING_LEAVE → INACTIVE preserving STAKING_HOOK.
		runTssReshareExcluding(t, app, ctx, validators[0])

		uv, err := app.UvalidatorKeeper.UniversalValidatorSet.Get(ctx, valAddr0)
		require.NoError(t, err)
		require.Equal(t, uvalidatortypes.UVStatus_UV_STATUS_INACTIVE, uv.LifecycleInfo.CurrentStatus)
		latest := uv.LifecycleInfo.History[len(uv.LifecycleInfo.History)-1]
		require.Equal(t, uvalidatortypes.TransitionReason_TRANSITION_REASON_STAKING_HOOK, latest.Reason,
			"TSS finalization must propagate STAKING_HOOK reason from prior PENDING_LEAVE event")
	})

	t.Run("PENDING_LEAVE with ADMIN → INACTIVE preserves ADMIN", func(t *testing.T) {
		app, ctx, validators := setupTssKeyProcessTest(t, 3)
		valAddr0 := mustValAddr(t, validators[0])

		require.NoError(t, app.UvalidatorKeeper.UpdateValidatorStatus(ctx, valAddr0,
			uvalidatortypes.UVStatus_UV_STATUS_PENDING_LEAVE,
			uvalidatortypes.TransitionReason_TRANSITION_REASON_ADMIN))

		runTssReshareExcluding(t, app, ctx, validators[0])

		uv, err := app.UvalidatorKeeper.UniversalValidatorSet.Get(ctx, valAddr0)
		require.NoError(t, err)
		require.Equal(t, uvalidatortypes.UVStatus_UV_STATUS_INACTIVE, uv.LifecycleInfo.CurrentStatus)
		latest := uv.LifecycleInfo.History[len(uv.LifecycleInfo.History)-1]
		require.Equal(t, uvalidatortypes.TransitionReason_TRANSITION_REASON_ADMIN, latest.Reason,
			"TSS finalization must propagate ADMIN reason from prior PENDING_LEAVE event")
	})
}

// TestStakingHook_FullLifecycle_HookThenTssFinalizeThenRebond is the F-2026-16991
// load-bearing end-to-end: a hook-driven removal that runs the full pipeline
// (hook → TSS reshare → finalization → re-bond) and asserts the reason field
// survives every transition so the auto-revival logic still fires correctly.
//
//   1. ACTIVE  → AfterValidatorBeginUnbonding fires  → PENDING_LEAVE/STAKING_HOOK
//   2. TSS reshare runs to completion                → PENDING_LEAVE → INACTIVE/STAKING_HOOK
//   3. AfterValidatorBonded fires (re-bond)          → INACTIVE → PENDING_JOIN/STAKING_HOOK
//
// Without the reason-propagation in step 2, step 3 would see INACTIVE+UNSPECIFIED
// and refuse to revive — silently breaking the auto-revival contract.
func TestStakingHook_FullLifecycle_HookThenTssFinalizeThenRebond(t *testing.T) {
	app, ctx, validators := setupTssKeyProcessTest(t, 3)
	valAddr0 := mustValAddr(t, validators[0])
	consAddr0, err := sdk.ValAddressFromBech32(validators[0])
	require.NoError(t, err)

	// All 3 validators are PENDING_JOIN from setupTssKeyProcessTest's keygen.
	// Promote val[0] to ACTIVE so we can exercise the ACTIVE branch of the hook.
	require.NoError(t, app.UvalidatorKeeper.UpdateValidatorStatus(ctx, valAddr0,
		uvalidatortypes.UVStatus_UV_STATUS_ACTIVE,
		uvalidatortypes.TransitionReason_TRANSITION_REASON_UNSPECIFIED))

	h := app.UvalidatorKeeper.StakingHooks()

	// Step 1: hook fires.
	require.NoError(t, h.AfterValidatorBeginUnbonding(ctx, sdk.ConsAddress(consAddr0), valAddr0))
	uv, _ := app.UvalidatorKeeper.UniversalValidatorSet.Get(ctx, valAddr0)
	require.Equal(t, uvalidatortypes.UVStatus_UV_STATUS_PENDING_LEAVE, uv.LifecycleInfo.CurrentStatus)
	require.Equal(t, uvalidatortypes.TransitionReason_TRANSITION_REASON_STAKING_HOOK,
		uv.LifecycleInfo.History[len(uv.LifecycleInfo.History)-1].Reason)

	// Step 2: TSS reshare excluding val[0] runs to completion.
	runTssReshareExcluding(t, app, ctx, validators[0])
	uv, _ = app.UvalidatorKeeper.UniversalValidatorSet.Get(ctx, valAddr0)
	require.Equal(t, uvalidatortypes.UVStatus_UV_STATUS_INACTIVE, uv.LifecycleInfo.CurrentStatus)
	require.Equal(t, uvalidatortypes.TransitionReason_TRANSITION_REASON_STAKING_HOOK,
		uv.LifecycleInfo.History[len(uv.LifecycleInfo.History)-1].Reason,
		"STAKING_HOOK reason must survive TSS finalization (load-bearing for revival)")

	// Step 3: validator re-bonds → auto-revives to PENDING_JOIN.
	require.NoError(t, h.AfterValidatorBonded(ctx, sdk.ConsAddress(consAddr0), valAddr0))
	uv, _ = app.UvalidatorKeeper.UniversalValidatorSet.Get(ctx, valAddr0)
	require.Equal(t, uvalidatortypes.UVStatus_UV_STATUS_PENDING_JOIN, uv.LifecycleInfo.CurrentStatus,
		"INACTIVE UV with STAKING_HOOK reason must auto-revive to PENDING_JOIN on re-bond")
	require.Equal(t, uvalidatortypes.TransitionReason_TRANSITION_REASON_STAKING_HOOK,
		uv.LifecycleInfo.History[len(uv.LifecycleInfo.History)-1].Reason)
}
