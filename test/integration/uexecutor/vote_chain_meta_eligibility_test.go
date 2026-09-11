package integrationtest

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/app"
	utils "github.com/pushchain/push-chain-node/test/utils"
	uvalidatortypes "github.com/pushchain/push-chain-node/x/uvalidator/types"
)

// coreAccOf returns the account bech32 that signs MsgVoteChainMeta on behalf of
// the given staking validator (the hotkey's grantee target).
func coreAccOf(t *testing.T, val stakingtypes.Validator) string {
	t.Helper()
	valAddr, err := sdk.ValAddressFromBech32(val.OperatorAddress)
	require.NoError(t, err)
	return sdk.AccAddress(valAddr).String()
}

// forceUVLifecycleStatus overwrites only the lifecycle status of an already
// registered universal validator, leaving identity/network info intact and
// leaving the underlying staking validator bonded. It bypasses transition
// validation so INACTIVE can be reached directly.
func forceUVLifecycleStatus(
	t *testing.T,
	testApp *app.ChainApp,
	ctx sdk.Context,
	val stakingtypes.Validator,
	status uvalidatortypes.UVStatus,
) {
	t.Helper()
	valAddr, err := sdk.ValAddressFromBech32(val.OperatorAddress)
	require.NoError(t, err)

	uv, err := testApp.UvalidatorKeeper.UniversalValidatorSet.Get(ctx, valAddr)
	require.NoError(t, err)
	uv.LifecycleInfo.CurrentStatus = status
	require.NoError(t, testApp.UvalidatorKeeper.UniversalValidatorSet.Set(ctx, valAddr, uv))
}

// requireStillBonded asserts the staking validator behind a universal validator
// is still bonded. This is the precondition the finding rests on: lifecycle
// removal does not unbond stake, so a bonded-only admission gate keeps letting
// the removed hotkey in.
func requireStillBonded(t *testing.T, testApp *app.ChainApp, ctx sdk.Context, val stakingtypes.Validator) {
	t.Helper()
	valAddr, err := sdk.ValAddressFromBech32(val.OperatorAddress)
	require.NoError(t, err)
	sv, err := testApp.StakingKeeper.GetValidator(ctx, valAddr)
	require.NoError(t, err)
	require.True(t, sv.IsBonded(),
		"removal must leave the validator bonded -- otherwise the finding's vector would not exist")
}

// TestVoteChainMeta_EligibilityGate is the F-2026-18148 regression suite.
//
// MsgVoteChainMeta used to admit any bonded, registered universal validator.
// Admin removal moves a universal validator to PENDING_LEAVE while its stake
// stays bonded, and AfterValidatorRemoved prunes its ChainMeta rows but revokes
// neither its AuthZ grant nor its membership in the set -- so the removed
// hotkey could reinsert votes straight after the prune. Admission is now gated
// on the same eligibility predicate (ACTIVE / PENDING_JOIN + bonded + not
// tombstoned) that uvalidator uses to snapshot ballot voters.
func TestVoteChainMeta_EligibilityGate(t *testing.T) {
	chainId := "eip155:11155111"

	t.Run("removed PENDING_LEAVE validator cannot reinsert a vote after the prune", func(t *testing.T) {
		testApp, ctx, uvals, vals := setupVoteChainMetaTest(t, 5)

		// Removal of an ACTIVE universal validator requires no ongoing TSS.
		_ = testApp.UtssKeeper.CurrentTssProcess.Remove(ctx)
		for _, v := range vals {
			valAddr, err := sdk.ValAddressFromBech32(v.OperatorAddress)
			require.NoError(t, err)
			require.NoError(t, testApp.UvalidatorKeeper.UpdateValidatorStatus(
				ctx, valAddr,
				uvalidatortypes.UVStatus_UV_STATUS_ACTIVE,
				uvalidatortypes.TransitionReason_TRANSITION_REASON_UNSPECIFIED,
			))
		}

		coreAccs := make([]string, len(vals))
		for i := range vals {
			coreAccs[i] = coreAccOf(t, vals[i])
		}

		// Five ACTIVE validators vote. Prices 100..500, heights 10..50.
		// After the 3rd vote the oracle bootstraps; by the 5th the recorded
		// upper median price is 300 and LastAppliedChainHeight is 30.
		prices := []uint64{100, 200, 300, 400, 500}
		heights := []uint64{10, 20, 30, 40, 50}
		for i := range vals {
			require.NoError(t, utils.ExecVoteChainMeta(t, ctx, testApp, uvals[i], coreAccs[i], chainId, prices[i], heights[i]))
		}

		stored, found, err := testApp.UexecutorKeeper.GetChainMeta(ctx, chainId)
		require.NoError(t, err)
		require.True(t, found)
		require.Len(t, stored.Signers, 5)
		require.Equal(t, uint64(300), stored.Prices[stored.MedianIndex], "baseline recorded median price")
		require.Equal(t, uint64(30), stored.LastAppliedChainHeight, "baseline applied chain height")

		// Admin removes validator 4: ACTIVE -> PENDING_LEAVE, ChainMeta pruned.
		require.NoError(t, testApp.UvalidatorKeeper.RemoveUniversalValidator(ctx, vals[4].OperatorAddress))

		removedValAddr, err := sdk.ValAddressFromBech32(vals[4].OperatorAddress)
		require.NoError(t, err)
		uv, uvFound, err := testApp.UvalidatorKeeper.GetUniversalValidator(ctx, removedValAddr)
		require.NoError(t, err)
		require.True(t, uvFound, "removal keeps the row in the set -- only the lifecycle status changes")
		require.Equal(t, uvalidatortypes.UVStatus_UV_STATUS_PENDING_LEAVE, uv.LifecycleInfo.CurrentStatus)

		// The two halves of the vector: stake is still bonded, and the AuthZ
		// grant was never revoked, so the hotkey can still build the tx.
		requireStillBonded(t, testApp, ctx, vals[4])

		pruned, _, err := testApp.UexecutorKeeper.GetChainMeta(ctx, chainId)
		require.NoError(t, err)
		require.Len(t, pruned.Signers, 4, "the removed validator's ChainMeta row must have been pruned")

		// The removed hotkey now tries to reinsert a vote. A price of 250 sits
		// between the surviving 200 and 300, so if it landed it would drag the
		// upper median down from 300 to 250. Height 35 clears the stale-height
		// gate (LastAppliedChainHeight = 30).
		reinsertErr := utils.ExecVoteChainMeta(t, ctx, testApp, uvals[4], coreAccs[4], chainId, 250, 35)

		after, _, err := testApp.UexecutorKeeper.GetChainMeta(ctx, chainId)
		require.NoError(t, err)

		// State assertions first: an aborting error assertion must not be able
		// to hide a vote that actually landed.
		require.Equal(t, uint64(300), after.Prices[after.MedianIndex],
			"the median must still be computed over the four surviving votes only")
		require.NotContains(t, after.Signers, removedValAddr.String(),
			"the removed validator must not reappear among the ChainMeta signers")
		require.NotContains(t, after.Prices, uint64(250), "the rejected price must not be recorded")
		require.Len(t, after.Signers, 4, "no new signer row may be inserted")
		require.Equal(t, uint64(30), after.LastAppliedChainHeight,
			"a rejected vote must not advance the applied chain height")

		require.Error(t, reinsertErr, "a PENDING_LEAVE universal validator must not be able to vote on chain meta")
		require.Contains(t, reinsertErr.Error(), "is not an eligible voter")
	})

	t.Run("INACTIVE but still-bonded validator is rejected", func(t *testing.T) {
		testApp, ctx, uvals, vals := setupVoteChainMetaTest(t, 3)

		for _, v := range vals {
			valAddr, err := sdk.ValAddressFromBech32(v.OperatorAddress)
			require.NoError(t, err)
			require.NoError(t, testApp.UvalidatorKeeper.UpdateValidatorStatus(
				ctx, valAddr,
				uvalidatortypes.UVStatus_UV_STATUS_ACTIVE,
				uvalidatortypes.TransitionReason_TRANSITION_REASON_UNSPECIFIED,
			))
		}
		forceUVLifecycleStatus(t, testApp, ctx, vals[2], uvalidatortypes.UVStatus_UV_STATUS_INACTIVE)
		requireStillBonded(t, testApp, ctx, vals[2])

		voteErr := utils.ExecVoteChainMeta(t, ctx, testApp, uvals[2], coreAccOf(t, vals[2]), chainId, 777, 7)

		_, found, err := testApp.UexecutorKeeper.GetChainMeta(ctx, chainId)
		require.NoError(t, err)
		require.False(t, found, "an INACTIVE validator's vote must not create a ChainMeta entry")

		require.Error(t, voteErr, "an INACTIVE universal validator must not be able to vote on chain meta")
		require.Contains(t, voteErr.Error(), "is not an eligible voter")
	})

	t.Run("ACTIVE validator is still accepted", func(t *testing.T) {
		testApp, ctx, uvals, vals := setupVoteChainMetaTest(t, 3)

		for _, v := range vals {
			valAddr, err := sdk.ValAddressFromBech32(v.OperatorAddress)
			require.NoError(t, err)
			require.NoError(t, testApp.UvalidatorKeeper.UpdateValidatorStatus(
				ctx, valAddr,
				uvalidatortypes.UVStatus_UV_STATUS_ACTIVE,
				uvalidatortypes.TransitionReason_TRANSITION_REASON_UNSPECIFIED,
			))
		}

		require.NoError(t, utils.ExecVoteChainMeta(t, ctx, testApp, uvals[0], coreAccOf(t, vals[0]), chainId, 100, 1))

		stored, found, err := testApp.UexecutorKeeper.GetChainMeta(ctx, chainId)
		require.NoError(t, err)
		require.True(t, found)
		require.Len(t, stored.Signers, 1, "the ACTIVE validator's vote must be recorded")
	})

	t.Run("PENDING_JOIN validator is still accepted", func(t *testing.T) {
		// setupVoteChainMetaTest registers every universal validator through
		// AddUniversalValidator, which leaves them in PENDING_JOIN.
		testApp, ctx, uvals, vals := setupVoteChainMetaTest(t, 3)

		valAddr, err := sdk.ValAddressFromBech32(vals[1].OperatorAddress)
		require.NoError(t, err)
		uv, found, err := testApp.UvalidatorKeeper.GetUniversalValidator(ctx, valAddr)
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, uvalidatortypes.UVStatus_UV_STATUS_PENDING_JOIN, uv.LifecycleInfo.CurrentStatus)

		require.NoError(t, utils.ExecVoteChainMeta(t, ctx, testApp, uvals[1], coreAccOf(t, vals[1]), chainId, 100, 1))

		stored, found, err := testApp.UexecutorKeeper.GetChainMeta(ctx, chainId)
		require.NoError(t, err)
		require.True(t, found)
		require.Len(t, stored.Signers, 1, "the PENDING_JOIN validator's vote must be recorded")
	})

	t.Run("fewer than three eligible validators cannot reach the bootstrap minimum", func(t *testing.T) {
		// Documents the bootstrap interaction, it does not assert a defect:
		// chainMetaMinVotesForFirstWrite = 3 counts fresh vote ROWS, and there
		// is at most one row per validator. Tightening admission can only
		// shrink the pool of validators able to produce a row, so a set with
		// fewer than three ELIGIBLE universal validators can never bootstrap
		// the oracle. That was already true of any topology with fewer than
		// three bonded universal validators; this gate makes lifecycle state
		// count towards it too.
		testApp, ctx, uvals, vals := setupVoteChainMetaTest(t, 3)

		for _, v := range vals {
			valAddr, err := sdk.ValAddressFromBech32(v.OperatorAddress)
			require.NoError(t, err)
			require.NoError(t, testApp.UvalidatorKeeper.UpdateValidatorStatus(
				ctx, valAddr,
				uvalidatortypes.UVStatus_UV_STATUS_ACTIVE,
				uvalidatortypes.TransitionReason_TRANSITION_REASON_UNSPECIFIED,
			))
		}
		forceUVLifecycleStatus(t, testApp, ctx, vals[2], uvalidatortypes.UVStatus_UV_STATUS_PENDING_LEAVE)
		requireStillBonded(t, testApp, ctx, vals[2])

		require.NoError(t, utils.ExecVoteChainMeta(t, ctx, testApp, uvals[0], coreAccOf(t, vals[0]), chainId, 100, 1))
		require.NoError(t, utils.ExecVoteChainMeta(t, ctx, testApp, uvals[1], coreAccOf(t, vals[1]), chainId, 200, 2))
		thirdErr := utils.ExecVoteChainMeta(t, ctx, testApp, uvals[2], coreAccOf(t, vals[2]), chainId, 300, 3)

		stored, found, err := testApp.UexecutorKeeper.GetChainMeta(ctx, chainId)
		require.NoError(t, err)
		require.True(t, found)
		require.Len(t, stored.Signers, 2, "only the two eligible validators may hold a vote row")
		require.Equal(t, uint64(0), stored.LastAppliedChainHeight,
			"two fresh votes are below chainMetaMinVotesForFirstWrite=3, so the oracle stays un-bootstrapped")

		require.Error(t, thirdErr)
		require.Contains(t, thirdErr.Error(), "is not an eligible voter")
	})
}
