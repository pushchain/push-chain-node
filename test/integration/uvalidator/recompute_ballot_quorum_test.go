package integrationtest

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"

	uvalidatorkeepermod "github.com/pushchain/push-chain-node/x/uvalidator/keeper"
	uvalidatortypes "github.com/pushchain/push-chain-node/x/uvalidator/types"
)

// makeStuckBallot creates a PENDING ballot with the supplied eligible voters
// (valoper bech32) and the auto-computed 2/3+1 threshold. Returns the ballot.
func makeStuckBallot(t *testing.T, ballotID string, eligibleVoters []string, votes []uvalidatortypes.VoteResult) uvalidatortypes.Ballot {
	t.Helper()
	threshold := int64((2*len(eligibleVoters))/3 + 1)
	if len(votes) == 0 {
		votes = make([]uvalidatortypes.VoteResult, len(eligibleVoters))
	}
	return uvalidatortypes.Ballot{
		Id:                 ballotID,
		BallotType:         uvalidatortypes.BallotObservationType_BALLOT_OBSERVATION_TYPE_INBOUND_TX,
		EligibleVoters:     eligibleVoters,
		Votes:              votes,
		VotingThreshold:    threshold,
		Status:             uvalidatortypes.BallotStatus_BALLOT_STATUS_PENDING,
		BlockHeightCreated: 1,
		BlockHeightExpiry:  100_000_000,
	}
}

func TestRecomputeBallotQuorum_HappyPath_ShrinksThreshold(t *testing.T) {
	chainApp, ctx, validators := setupQueryTest(t, 5)

	// All ACTIVE so they're in the eligible set.
	for _, v := range validators {
		setUVStatus(t, chainApp, ctx, v, uvalidatortypes.UVStatus_UV_STATUS_ACTIVE)
	}

	// Seed admin so signer-auth check has something to compare against.
	const admin = "push1fgaewhyd9fkwtqaj9c233letwcuey6dgly9gv9"
	require.NoError(t, chainApp.UvalidatorKeeper.Params.Set(ctx, uvalidatortypes.Params{Admin: admin}))

	// Build a stuck ballot with all 5 voters and threshold 4. One vote so far.
	voterStrs := make([]string, len(validators))
	for i, v := range validators {
		voterStrs[i] = v.OperatorAddress
	}
	votes := make([]uvalidatortypes.VoteResult, len(validators))
	votes[0] = uvalidatortypes.VoteResult_VOTE_RESULT_SUCCESS
	ballot := makeStuckBallot(t, "stuck-ballot-1", voterStrs, votes)
	require.NoError(t, chainApp.UvalidatorKeeper.Ballots.Set(ctx, ballot.Id, ballot))
	require.NoError(t, chainApp.UvalidatorKeeper.ActiveBallotIDs.Set(ctx, ballot.Id))

	// Strand 3 validators on the base chain (unbonded). #2 filter excludes them.
	for i := 0; i < 3; i++ {
		v := validators[i]
		v.Status = stakingtypes.Unbonded
		require.NoError(t, chainApp.StakingKeeper.SetValidator(ctx, v))
	}

	// Sanity: GetEligibleVoters now returns 2.
	eligible, err := chainApp.UvalidatorKeeper.GetEligibleVoters(ctx)
	require.NoError(t, err)
	require.Len(t, eligible, 2, "filter should exclude 3 stranded UVs")

	// Recompute.
	oldEligible, newEligible, oldThreshold, newThreshold, newStatus, err :=
		chainApp.UvalidatorKeeper.RecomputeBallotQuorum(ctx, ballot.Id)
	require.NoError(t, err)
	require.Equal(t, int64(5), oldEligible)
	require.Equal(t, int64(2), newEligible)
	require.Equal(t, int64(4), oldThreshold)
	require.Equal(t, int64(2), newThreshold, "new threshold = (2*2)/3 + 1 = 2")
	require.Equal(t, uvalidatortypes.BallotStatus_BALLOT_STATUS_PENDING, newStatus)

	// Verify persisted ballot.
	updated, err := chainApp.UvalidatorKeeper.Ballots.Get(ctx, ballot.Id)
	require.NoError(t, err)
	require.Len(t, updated.EligibleVoters, 2)
	require.Len(t, updated.Votes, 2)
	require.Equal(t, int64(2), updated.VotingThreshold)
	require.Equal(t, uvalidatortypes.BallotStatus_BALLOT_STATUS_PENDING, updated.Status)
}

func TestRecomputeBallotQuorum_PreservesVotesFromStillEligibleVoters(t *testing.T) {
	chainApp, ctx, validators := setupQueryTest(t, 5)
	for _, v := range validators {
		setUVStatus(t, chainApp, ctx, v, uvalidatortypes.UVStatus_UV_STATUS_ACTIVE)
	}

	// validators[0]=SUCCESS, validators[1]=FAILURE, validators[2-4]=NOT_YET
	voterStrs := []string{
		validators[0].OperatorAddress,
		validators[1].OperatorAddress,
		validators[2].OperatorAddress,
		validators[3].OperatorAddress,
		validators[4].OperatorAddress,
	}
	votes := []uvalidatortypes.VoteResult{
		uvalidatortypes.VoteResult_VOTE_RESULT_SUCCESS,
		uvalidatortypes.VoteResult_VOTE_RESULT_FAILURE,
		uvalidatortypes.VoteResult_VOTE_RESULT_NOT_YET_VOTED,
		uvalidatortypes.VoteResult_VOTE_RESULT_NOT_YET_VOTED,
		uvalidatortypes.VoteResult_VOTE_RESULT_NOT_YET_VOTED,
	}
	ballot := makeStuckBallot(t, "stuck-ballot-2", voterStrs, votes)
	require.NoError(t, chainApp.UvalidatorKeeper.Ballots.Set(ctx, ballot.Id, ballot))
	require.NoError(t, chainApp.UvalidatorKeeper.ActiveBallotIDs.Set(ctx, ballot.Id))

	// Unbond validators[2] only (NOT_YET vote — dropped silently). validators[0] and [1] stay.
	v2 := validators[2]
	v2.Status = stakingtypes.Unbonded
	require.NoError(t, chainApp.StakingKeeper.SetValidator(ctx, v2))

	_, _, _, _, _, err := chainApp.UvalidatorKeeper.RecomputeBallotQuorum(ctx, ballot.Id)
	require.NoError(t, err)

	updated, _ := chainApp.UvalidatorKeeper.Ballots.Get(ctx, ballot.Id)
	require.Len(t, updated.EligibleVoters, 4, "4 still eligible")

	// validators[0]'s SUCCESS and validators[1]'s FAILURE must survive.
	voteMap := map[string]uvalidatortypes.VoteResult{}
	for i, voter := range updated.EligibleVoters {
		voteMap[voter] = updated.Votes[i]
	}
	require.Equal(t, uvalidatortypes.VoteResult_VOTE_RESULT_SUCCESS, voteMap[validators[0].OperatorAddress])
	require.Equal(t, uvalidatortypes.VoteResult_VOTE_RESULT_FAILURE, voteMap[validators[1].OperatorAddress])
}

func TestRecomputeBallotQuorum_DropsVotesFromIneligibleVoters(t *testing.T) {
	chainApp, ctx, validators := setupQueryTest(t, 3)
	for _, v := range validators {
		setUVStatus(t, chainApp, ctx, v, uvalidatortypes.UVStatus_UV_STATUS_ACTIVE)
	}

	voterStrs := []string{validators[0].OperatorAddress, validators[1].OperatorAddress, validators[2].OperatorAddress}
	votes := []uvalidatortypes.VoteResult{
		uvalidatortypes.VoteResult_VOTE_RESULT_SUCCESS,
		uvalidatortypes.VoteResult_VOTE_RESULT_SUCCESS,
		uvalidatortypes.VoteResult_VOTE_RESULT_NOT_YET_VOTED,
	}
	ballot := makeStuckBallot(t, "stuck-ballot-3", voterStrs, votes)
	require.NoError(t, chainApp.UvalidatorKeeper.Ballots.Set(ctx, ballot.Id, ballot))
	require.NoError(t, chainApp.UvalidatorKeeper.ActiveBallotIDs.Set(ctx, ballot.Id))

	// Strand the two SUCCESS voters. Only validators[2] remains.
	for i := 0; i < 2; i++ {
		v := validators[i]
		v.Status = stakingtypes.Unbonded
		require.NoError(t, chainApp.StakingKeeper.SetValidator(ctx, v))
	}

	_, newEligible, _, _, _, err := chainApp.UvalidatorKeeper.RecomputeBallotQuorum(ctx, ballot.Id)
	require.NoError(t, err)
	require.Equal(t, int64(1), newEligible)

	updated, _ := chainApp.UvalidatorKeeper.Ballots.Get(ctx, ballot.Id)
	require.Len(t, updated.EligibleVoters, 1)
	require.Equal(t, validators[2].OperatorAddress, updated.EligibleVoters[0])
	// The remaining voter's NOT_YET vote is preserved (was NOT_YET in old list).
	require.Equal(t, uvalidatortypes.VoteResult_VOTE_RESULT_NOT_YET_VOTED, updated.Votes[0])
}

func TestRecomputeBallotQuorum_ZeroEligible_MarksExpired(t *testing.T) {
	chainApp, ctx, validators := setupQueryTest(t, 3)
	for _, v := range validators {
		setUVStatus(t, chainApp, ctx, v, uvalidatortypes.UVStatus_UV_STATUS_ACTIVE)
	}

	voterStrs := []string{validators[0].OperatorAddress, validators[1].OperatorAddress, validators[2].OperatorAddress}
	ballot := makeStuckBallot(t, "stuck-ballot-4", voterStrs, nil)
	require.NoError(t, chainApp.UvalidatorKeeper.Ballots.Set(ctx, ballot.Id, ballot))
	require.NoError(t, chainApp.UvalidatorKeeper.ActiveBallotIDs.Set(ctx, ballot.Id))

	// Unbond all 3.
	for _, v := range validators {
		v.Status = stakingtypes.Unbonded
		require.NoError(t, chainApp.StakingKeeper.SetValidator(ctx, v))
	}

	_, newEligible, _, newThreshold, newStatus, err :=
		chainApp.UvalidatorKeeper.RecomputeBallotQuorum(ctx, ballot.Id)
	require.NoError(t, err)
	require.Equal(t, int64(0), newEligible)
	require.Equal(t, int64(0), newThreshold)
	require.Equal(t, uvalidatortypes.BallotStatus_BALLOT_STATUS_EXPIRED, newStatus)

	// Ballot is now EXPIRED.
	updated, _ := chainApp.UvalidatorKeeper.Ballots.Get(ctx, ballot.Id)
	require.Equal(t, uvalidatortypes.BallotStatus_BALLOT_STATUS_EXPIRED, updated.Status)

	// Moved out of active index, into expired index.
	hasActive, _ := chainApp.UvalidatorKeeper.ActiveBallotIDs.Has(ctx, ballot.Id)
	require.False(t, hasActive)
	hasExpired, _ := chainApp.UvalidatorKeeper.ExpiredBallotIDs.Has(ctx, ballot.Id)
	require.True(t, hasExpired)
}

func TestRecomputeBallotQuorum_NonExistentBallot(t *testing.T) {
	chainApp, ctx, _ := setupQueryTest(t, 3)

	_, _, _, _, _, err := chainApp.UvalidatorKeeper.RecomputeBallotQuorum(ctx, "nonexistent-ballot")
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestRecomputeBallotQuorum_AlreadyFinalizedBallot_Rejected(t *testing.T) {
	chainApp, ctx, validators := setupQueryTest(t, 3)

	voterStrs := []string{validators[0].OperatorAddress, validators[1].OperatorAddress, validators[2].OperatorAddress}
	ballot := makeStuckBallot(t, "passed-ballot", voterStrs, nil)
	ballot.Status = uvalidatortypes.BallotStatus_BALLOT_STATUS_PASSED
	require.NoError(t, chainApp.UvalidatorKeeper.Ballots.Set(ctx, ballot.Id, ballot))

	_, _, _, _, _, err := chainApp.UvalidatorKeeper.RecomputeBallotQuorum(ctx, ballot.Id)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not pending")
}

func TestRecomputeBallotQuorum_NoDrift_IsIdempotent(t *testing.T) {
	chainApp, ctx, validators := setupQueryTest(t, 3)
	for _, v := range validators {
		setUVStatus(t, chainApp, ctx, v, uvalidatortypes.UVStatus_UV_STATUS_ACTIVE)
	}

	voterStrs := []string{validators[0].OperatorAddress, validators[1].OperatorAddress, validators[2].OperatorAddress}
	votes := []uvalidatortypes.VoteResult{
		uvalidatortypes.VoteResult_VOTE_RESULT_SUCCESS,
		uvalidatortypes.VoteResult_VOTE_RESULT_NOT_YET_VOTED,
		uvalidatortypes.VoteResult_VOTE_RESULT_NOT_YET_VOTED,
	}
	ballot := makeStuckBallot(t, "no-drift-ballot", voterStrs, votes)
	require.NoError(t, chainApp.UvalidatorKeeper.Ballots.Set(ctx, ballot.Id, ballot))
	require.NoError(t, chainApp.UvalidatorKeeper.ActiveBallotIDs.Set(ctx, ballot.Id))

	// No drift — all 3 still active+bonded.
	oldEligible, newEligible, oldThreshold, newThreshold, _, err :=
		chainApp.UvalidatorKeeper.RecomputeBallotQuorum(ctx, ballot.Id)
	require.NoError(t, err)
	require.Equal(t, oldEligible, newEligible, "no-drift recompute leaves count unchanged")
	require.Equal(t, oldThreshold, newThreshold, "no-drift recompute leaves threshold unchanged")

	updated, _ := chainApp.UvalidatorKeeper.Ballots.Get(ctx, ballot.Id)
	require.Equal(t, uvalidatortypes.VoteResult_VOTE_RESULT_SUCCESS, updated.Votes[0], "existing vote preserved")
}

func TestRecomputeBallotQuorum_AdminAuth_RejectsNonAdmin(t *testing.T) {
	chainApp, ctx, validators := setupQueryTest(t, 3)

	const admin = "push1fgaewhyd9fkwtqaj9c233letwcuey6dgly9gv9"
	const notAdmin = "push1negskcfqu09j5zvpk7nhvacnwyy2mafffy7r6a"
	require.NoError(t, chainApp.UvalidatorKeeper.Params.Set(ctx, uvalidatortypes.Params{Admin: admin}))

	voterStrs := []string{validators[0].OperatorAddress}
	ballot := makeStuckBallot(t, "auth-test-ballot", voterStrs, nil)
	require.NoError(t, chainApp.UvalidatorKeeper.Ballots.Set(ctx, ballot.Id, ballot))

	ms := uvalidatorkeepermod.NewMsgServerImpl(chainApp.UvalidatorKeeper)
	_, err := ms.RecomputeBallotQuorum(sdk.WrapSDKContext(ctx), &uvalidatortypes.MsgRecomputeBallotQuorum{
		Signer:   notAdmin,
		BallotId: ballot.Id,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid admin")
}

func TestRecomputeBallotQuorum_AdminAuth_AcceptsAdmin(t *testing.T) {
	chainApp, ctx, validators := setupQueryTest(t, 3)
	for _, v := range validators {
		setUVStatus(t, chainApp, ctx, v, uvalidatortypes.UVStatus_UV_STATUS_ACTIVE)
	}

	const admin = "push1fgaewhyd9fkwtqaj9c233letwcuey6dgly9gv9"
	require.NoError(t, chainApp.UvalidatorKeeper.Params.Set(ctx, uvalidatortypes.Params{Admin: admin}))

	voterStrs := []string{validators[0].OperatorAddress, validators[1].OperatorAddress, validators[2].OperatorAddress}
	ballot := makeStuckBallot(t, "auth-accept-ballot", voterStrs, nil)
	require.NoError(t, chainApp.UvalidatorKeeper.Ballots.Set(ctx, ballot.Id, ballot))
	require.NoError(t, chainApp.UvalidatorKeeper.ActiveBallotIDs.Set(ctx, ballot.Id))

	ms := uvalidatorkeepermod.NewMsgServerImpl(chainApp.UvalidatorKeeper)
	resp, err := ms.RecomputeBallotQuorum(sdk.WrapSDKContext(ctx), &uvalidatortypes.MsgRecomputeBallotQuorum{
		Signer:   admin,
		BallotId: ballot.Id,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, int64(3), resp.NewEligibleCount)
}
