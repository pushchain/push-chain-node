package integrationtest

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/app"
	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
	uvalidatortypes "github.com/pushchain/push-chain-node/x/uvalidator/types"
)

// jailLikeSlashingBeginBlock reproduces exactly what x/slashing does to a
// validator during BeginBlock: it calls staking's Keeper.Jail, which runs
// jailValidator -> sets Validator.Jailed and deletes the power index, and
// never touches Validator.Status.
//
// Crucially it does NOT run staking's EndBlocker, so the bonded -> unbonding
// transition has not happened yet. That is the exact window every transaction
// in the block is processed in.
func jailLikeSlashingBeginBlock(t *testing.T, chainApp *app.ChainApp, ctx sdk.Context, val stakingtypes.Validator) stakingtypes.Validator {
	t.Helper()

	consAddr, err := val.GetConsAddr()
	require.NoError(t, err)
	require.NoError(t, chainApp.StakingKeeper.Jail(ctx, consAddr))

	valAddr, err := sdk.ValAddressFromBech32(val.OperatorAddress)
	require.NoError(t, err)
	jailed, err := chainApp.StakingKeeper.GetValidator(ctx, valAddr)
	require.NoError(t, err)
	return jailed
}

// TestGetEligibleVoters_ExcludesSameBlockJailedValidator is the F-2026-18133
// regression suite.
//
// Slashing jails in BeginBlock; staking moves the validator bonded ->
// unbonding only in EndBlocker. For the entire tx-processing phase in between,
// a jailed validator is both Jailed and IsBonded(). Before the fix that
// validator was snapshotted into a new ballot's EligibleVoters, so the frozen
// VotingThreshold ((2*N)/3 + 1) was computed on an inflated N while only N-1
// signers could actually vote -- stranding the ballot at N <= 3.
func TestGetEligibleVoters_ExcludesSameBlockJailedValidator(t *testing.T) {
	t.Run("precondition: a same-block jailed validator still reports IsBonded", func(t *testing.T) {
		// This subtest asserts the SDK behaviour the finding depends on. If it
		// ever stops holding, the fix below is redundant and this will say so.
		chainApp, ctx, validators := setupQueryTest(t, 3)
		for _, v := range validators {
			setUVStatus(t, chainApp, ctx, v, uvalidatortypes.UVStatus_UV_STATUS_ACTIVE)
		}

		jailed := jailLikeSlashingBeginBlock(t, chainApp, ctx, validators[0])

		require.True(t, jailed.IsJailed(), "staking.Jail must set Validator.Jailed")
		require.Equal(t, stakingtypes.Bonded, jailed.Status,
			"staking.Jail must NOT touch Validator.Status before EndBlocker")
		require.True(t, jailed.IsBonded(),
			"IsBonded() is GetStatus()==Bonded, so a jailed validator still passes it -- "+
				"this is precisely why an explicit Jailed gate is required")
	})

	t.Run("jailed validator is excluded from the eligible-voter set", func(t *testing.T) {
		chainApp, ctx, validators := setupQueryTest(t, 3)
		for _, v := range validators {
			setUVStatus(t, chainApp, ctx, v, uvalidatortypes.UVStatus_UV_STATUS_ACTIVE)
		}

		before, err := chainApp.UvalidatorKeeper.GetEligibleVoters(ctx)
		require.NoError(t, err)
		require.Len(t, before, 3, "all three are eligible before the jail")

		jailLikeSlashingBeginBlock(t, chainApp, ctx, validators[0])

		after, err := chainApp.UvalidatorKeeper.GetEligibleVoters(ctx)
		require.NoError(t, err)
		require.Len(t, after, 2, "the jailed validator must drop out of the eligible set")
		for _, v := range after {
			require.NotEqual(t, validators[0].OperatorAddress, v.IdentifyInfo.CoreValidatorAddress,
				"jailed validator must not appear among eligible voters")
		}
	})

	t.Run("PENDING_JOIN validator jailed in the same block is also excluded", func(t *testing.T) {
		// setupQueryTest leaves every UV in PENDING_JOIN, which is an eligible
		// lifecycle state. The Jailed gate must apply there too.
		chainApp, ctx, validators := setupQueryTest(t, 3)

		jailLikeSlashingBeginBlock(t, chainApp, ctx, validators[2])

		voters, err := chainApp.UvalidatorKeeper.GetEligibleVoters(ctx)
		require.NoError(t, err)
		require.Len(t, voters, 2)
		for _, v := range voters {
			require.NotEqual(t, validators[2].OperatorAddress, v.IdentifyInfo.CoreValidatorAddress)
		}
	})

	t.Run("ballot created in the same block freezes a threshold computed on N-1", func(t *testing.T) {
		// N = 3 is the worst reachable row from the finding: with the jailed
		// validator counted the threshold is (2*3)/3+1 = 3 against only 2
		// possible signers -> permanently stranded. With it excluded the
		// threshold is (2*2)/3+1 = 2 -> reachable.
		chainApp, ctx, validators := setupQueryTest(t, 3)
		for _, v := range validators {
			setUVStatus(t, chainApp, ctx, v, uvalidatortypes.UVStatus_UV_STATUS_ACTIVE)
		}

		// BeginBlock: slashing jails validators[0].
		jailLikeSlashingBeginBlock(t, chainApp, ctx, validators[0])

		// Same block, tx-processing phase: a surviving UV observes an inbound,
		// which creates the ballot and freezes EligibleVoters + VotingThreshold.
		ballot := voteInboundAndLoadBallot(t, chainApp, ctx, validators[1], sameBlockJailInbound)

		// Headline assertion first: the frozen threshold must be computed on
		// N-1. Everything else in this subtest is corroboration.
		require.Equal(t, int64(2), ballot.VotingThreshold,
			"threshold must be (2*2)/3+1 = 2 on the N-1 survivors, not (2*3)/3+1 = 3 on the inflated N")

		require.Len(t, ballot.EligibleVoters, 2,
			"the jailed validator must not be snapshotted into the ballot")
		require.NotContains(t, ballot.EligibleVoters, validators[0].OperatorAddress,
			"jailed validator address must be absent from the frozen voter snapshot")
		require.Contains(t, ballot.EligibleVoters, validators[1].OperatorAddress)
		require.Contains(t, ballot.EligibleVoters, validators[2].OperatorAddress)
	})

	t.Run("the surviving validators can still finalize that ballot", func(t *testing.T) {
		// The liveness half of the finding: with the jailed validator counted,
		// the frozen threshold of 3 is unreachable by the 2 survivors and the
		// ballot is stranded (only an admin MsgRecomputeBallotQuorum recovers
		// it, and DefaultExpiryAfterBlocks = 100_000_000 means it never ages
		// out on its own).
		chainApp, ctx, validators := setupQueryTest(t, 3)
		for _, v := range validators {
			setUVStatus(t, chainApp, ctx, v, uvalidatortypes.UVStatus_UV_STATUS_ACTIVE)
		}

		jailLikeSlashingBeginBlock(t, chainApp, ctx, validators[0])

		// First survivor votes: creates the ballot, does not finalize it.
		firstVoter, err := sdk.ValAddressFromBech32(validators[1].OperatorAddress)
		require.NoError(t, err)
		isFinalized, isNew, err := chainApp.UexecutorKeeper.VoteOnInboundBallot(ctx, firstVoter, sameBlockJailInbound)
		require.NoError(t, err)
		require.True(t, isNew, "the first vote must have created the ballot")
		require.False(t, isFinalized, "one vote out of a threshold of two must not finalize")

		// Second (and last) survivor votes: this must be the finalizing vote.
		secondVoter, err := sdk.ValAddressFromBech32(validators[2].OperatorAddress)
		require.NoError(t, err)
		isFinalized, isNew, err = chainApp.UexecutorKeeper.VoteOnInboundBallot(ctx, secondVoter, sameBlockJailInbound)
		require.NoError(t, err)
		require.False(t, isNew, "second vote must land on the existing ballot")
		require.True(t, isFinalized,
			"every non-jailed validator has now voted; if this is false the ballot is stranded "+
				"behind a threshold no reachable signer set can meet")

		ballotKey, err := uexecutortypes.GetInboundBallotKey(sameBlockJailInbound)
		require.NoError(t, err)
		ballot, err := chainApp.UvalidatorKeeper.Ballots.Get(ctx, ballotKey)
		require.NoError(t, err)
		require.Equal(t, uvalidatortypes.BallotStatus_BALLOT_STATUS_PASSED, ballot.Status,
			"the ballot must have reached a terminal PASSED status")
	})
}

// sameBlockJailInbound is the observation used by the ballot subtests above.
var sameBlockJailInbound = uexecutortypes.Inbound{
	SourceChain: "eip155:11155111",
	TxHash:      "0xf18133jailedquorum",
	LogIndex:    "0",
}

// voteInboundAndLoadBallot casts voter's inbound vote through the real
// uexecutor path (x/uexecutor/keeper/voting.go, the first of the seven
// GetEligibleVoters call sites) and returns the ballot it created.
func voteInboundAndLoadBallot(
	t *testing.T,
	chainApp *app.ChainApp,
	ctx sdk.Context,
	voter stakingtypes.Validator,
	inbound uexecutortypes.Inbound,
) uvalidatortypes.Ballot {
	t.Helper()

	voterAddr, err := sdk.ValAddressFromBech32(voter.OperatorAddress)
	require.NoError(t, err)

	_, isNew, err := chainApp.UexecutorKeeper.VoteOnInboundBallot(ctx, voterAddr, inbound)
	require.NoError(t, err)
	require.True(t, isNew, "the vote must have created the ballot")

	ballotKey, err := uexecutortypes.GetInboundBallotKey(inbound)
	require.NoError(t, err)
	ballot, err := chainApp.UvalidatorKeeper.Ballots.Get(ctx, ballotKey)
	require.NoError(t, err)
	return ballot
}
