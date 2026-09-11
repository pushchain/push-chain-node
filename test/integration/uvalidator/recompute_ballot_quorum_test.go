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

// ---------------------------------------------------------------------------
// F-2026-18793 — RecomputeBallotQuorum is type-aware (default-deny allow-list)
// ---------------------------------------------------------------------------

// makeTypedBallot builds a PENDING ballot of an arbitrary observation type with
// an explicit threshold, so TSS-style ballots (100% of the DKLS participant set,
// not 2/3+1) can be constructed exactly as x/utss creates them.
func makeTypedBallot(
	t *testing.T,
	ballotID string,
	ballotType uvalidatortypes.BallotObservationType,
	eligibleVoters []string,
	votes []uvalidatortypes.VoteResult,
	threshold int64,
) uvalidatortypes.Ballot {
	t.Helper()
	if len(votes) == 0 {
		votes = make([]uvalidatortypes.VoteResult, len(eligibleVoters))
	}
	return uvalidatortypes.Ballot{
		Id:                 ballotID,
		BallotType:         ballotType,
		EligibleVoters:     eligibleVoters,
		Votes:              votes,
		VotingThreshold:    threshold,
		Status:             uvalidatortypes.BallotStatus_BALLOT_STATUS_PENDING,
		BlockHeightCreated: 1,
		BlockHeightExpiry:  100_000_000,
	}
}

// Hacken rec 3 — the headline case. A TSS key ballot is created with a 100%
// quorum over the DKLS participants (votesNeeded = len(Participants)). An admin
// recompute must be refused outright: it would drop the threshold from 5 to
// (2*3)/3+1 = 3 AND swap the participant list for the live UV set, manufacturing
// an attestation the DKLS run never produced.
//
// The state assertions run BEFORE the error assertion on purpose: require.Error
// aborts the test on failure, so an error-first ordering would never reach the
// checks that catch a refusal which had already mutated state.
func TestRecomputeBallotQuorum_TSSKeyBallot_Refused_StateUnchanged(t *testing.T) {
	chainApp, ctx, validators := setupQueryTest(t, 5)
	for _, v := range validators {
		setUVStatus(t, chainApp, ctx, v, uvalidatortypes.UVStatus_UV_STATUS_ACTIVE)
	}

	// 5 DKLS participants, 100% quorum (threshold 5), 4 of 5 votes cast.
	participants := make([]string, len(validators))
	for i, v := range validators {
		participants[i] = v.OperatorAddress
	}
	votes := []uvalidatortypes.VoteResult{
		uvalidatortypes.VoteResult_VOTE_RESULT_SUCCESS,
		uvalidatortypes.VoteResult_VOTE_RESULT_SUCCESS,
		uvalidatortypes.VoteResult_VOTE_RESULT_SUCCESS,
		uvalidatortypes.VoteResult_VOTE_RESULT_SUCCESS,
		uvalidatortypes.VoteResult_VOTE_RESULT_NOT_YET_VOTED,
	}
	ballot := makeTypedBallot(t, "tss-key-ballot", uvalidatortypes.BallotObservationType_BALLOT_OBSERVATION_TYPE_TSS_KEY, participants, votes, 5)
	require.NoError(t, chainApp.UvalidatorKeeper.Ballots.Set(ctx, ballot.Id, ballot))
	require.NoError(t, chainApp.UvalidatorKeeper.ActiveBallotIDs.Set(ctx, ballot.Id))

	// Make the live UV set genuinely differ from the participant set, so an
	// unguarded recompute would visibly rewrite both threshold and eligibles.
	for i := 0; i < 2; i++ {
		v := validators[i]
		v.Status = stakingtypes.Unbonded
		require.NoError(t, chainApp.StakingKeeper.SetValidator(ctx, v))
	}
	eligibleNow, err := chainApp.UvalidatorKeeper.GetEligibleVoters(ctx)
	require.NoError(t, err)
	require.Len(t, eligibleNow, 3, "live UV set must differ from the DKLS participant set for this test to bite")

	_, _, _, _, _, recomputeErr := chainApp.UvalidatorKeeper.RecomputeBallotQuorum(ctx, ballot.Id)

	// --- state first ---
	after, getErr := chainApp.UvalidatorKeeper.Ballots.Get(ctx, ballot.Id)
	require.NoError(t, getErr)
	require.Equal(t, int64(5), after.VotingThreshold,
		"TSS threshold must stay at 100% of participants; a recompute would have set it to 3")
	require.Equal(t, participants, after.EligibleVoters,
		"the DKLS participant set must be byte-for-byte unchanged; a recompute would have swapped in the live UV set")
	require.Equal(t, votes, after.Votes, "votes must be untouched")
	require.Equal(t, uvalidatortypes.BallotStatus_BALLOT_STATUS_PENDING, after.Status,
		"refused recompute must not finalize the ballot")

	// --- then the refusal itself ---
	require.Error(t, recomputeErr, "recompute on a TSS_KEY ballot must be refused")
	require.Contains(t, recomputeErr.Error(), "cannot be recomputed")
	require.Contains(t, recomputeErr.Error(), "TSS_KEY")
}

// The three allow-listed types are created from GetEligibleVoters() with a
// (2*N)/3+1 threshold, so recompute reproduces creation exactly for them and
// must keep working unchanged.
func TestRecomputeBallotQuorum_AllowedTypes_StillRecompute(t *testing.T) {
	allowed := []uvalidatortypes.BallotObservationType{
		uvalidatortypes.BallotObservationType_BALLOT_OBSERVATION_TYPE_INBOUND_TX,
		uvalidatortypes.BallotObservationType_BALLOT_OBSERVATION_TYPE_OUTBOUND_TX,
		uvalidatortypes.BallotObservationType_BALLOT_OBSERVATION_TYPE_FUND_MIGRATION,
	}

	for _, bt := range allowed {
		t.Run(bt.String(), func(t *testing.T) {
			chainApp, ctx, validators := setupQueryTest(t, 5)
			for _, v := range validators {
				setUVStatus(t, chainApp, ctx, v, uvalidatortypes.UVStatus_UV_STATUS_ACTIVE)
			}

			voterStrs := make([]string, len(validators))
			for i, v := range validators {
				voterStrs[i] = v.OperatorAddress
			}
			ballot := makeTypedBallot(t, "allowed-"+bt.String(), bt, voterStrs, nil, 4)
			require.NoError(t, chainApp.UvalidatorKeeper.Ballots.Set(ctx, ballot.Id, ballot))
			require.NoError(t, chainApp.UvalidatorKeeper.ActiveBallotIDs.Set(ctx, ballot.Id))

			// Strand 3 → 2 eligible → threshold (2*2)/3+1 = 2.
			for i := 0; i < 3; i++ {
				v := validators[i]
				v.Status = stakingtypes.Unbonded
				require.NoError(t, chainApp.StakingKeeper.SetValidator(ctx, v))
			}

			oldEligible, newEligible, oldThreshold, newThreshold, newStatus, err :=
				chainApp.UvalidatorKeeper.RecomputeBallotQuorum(ctx, ballot.Id)
			require.NoError(t, err, "%s must remain recomputable", bt.String())
			require.Equal(t, int64(5), oldEligible)
			require.Equal(t, int64(2), newEligible)
			require.Equal(t, int64(4), oldThreshold)
			require.Equal(t, int64(2), newThreshold)
			require.Equal(t, uvalidatortypes.BallotStatus_BALLOT_STATUS_PENDING, newStatus)

			updated, err := chainApp.UvalidatorKeeper.Ballots.Get(ctx, ballot.Id)
			require.NoError(t, err)
			require.Equal(t, int64(2), updated.VotingThreshold)
			require.Len(t, updated.EligibleVoters, 2)
			require.Equal(t, []string{validators[3].OperatorAddress, validators[4].OperatorAddress}, updated.EligibleVoters)
		})
	}
}

// Pins the default-deny: UNSPECIFIED and a type value the switch has never seen
// are both refused. A future ballot type (e.g. READ_RESULT, which lands with the
// read-state branch) therefore inherits a refusal instead of silently inheriting
// the 2/3+1 formula.
func TestRecomputeBallotQuorum_UnrecognisedType_Refused(t *testing.T) {
	cases := []struct {
		name       string
		ballotType uvalidatortypes.BallotObservationType
	}{
		{"unspecified", uvalidatortypes.BallotObservationType_BALLOT_OBSERVATION_TYPE_UNSPECIFIED},
		{"future type not on the allow-list", uvalidatortypes.BallotObservationType(99)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			chainApp, ctx, validators := setupQueryTest(t, 3)
			for _, v := range validators {
				setUVStatus(t, chainApp, ctx, v, uvalidatortypes.UVStatus_UV_STATUS_ACTIVE)
			}

			voterStrs := []string{validators[0].OperatorAddress, validators[1].OperatorAddress, validators[2].OperatorAddress}
			ballot := makeTypedBallot(t, "unknown-type-"+tc.name, tc.ballotType, voterStrs, nil, 7)
			require.NoError(t, chainApp.UvalidatorKeeper.Ballots.Set(ctx, ballot.Id, ballot))
			require.NoError(t, chainApp.UvalidatorKeeper.ActiveBallotIDs.Set(ctx, ballot.Id))

			_, _, _, _, _, recomputeErr := chainApp.UvalidatorKeeper.RecomputeBallotQuorum(ctx, ballot.Id)

			// State first — see the TSS test for why the ordering matters.
			after, getErr := chainApp.UvalidatorKeeper.Ballots.Get(ctx, ballot.Id)
			require.NoError(t, getErr)
			require.Equal(t, int64(7), after.VotingThreshold, "threshold must be untouched by a refused recompute")
			require.Equal(t, voterStrs, after.EligibleVoters, "eligible voters must be untouched by a refused recompute")
			require.Equal(t, uvalidatortypes.BallotStatus_BALLOT_STATUS_PENDING, after.Status)

			require.Error(t, recomputeErr)
			require.Contains(t, recomputeErr.Error(), "cannot be recomputed")
		})
	}
}

// The PENDING-only guard still runs before the type check, so a non-pending
// ballot reports the status problem rather than the type problem.
func TestRecomputeBallotQuorum_StatusGuardRunsBeforeTypeGuard(t *testing.T) {
	chainApp, ctx, validators := setupQueryTest(t, 3)

	voterStrs := []string{validators[0].OperatorAddress, validators[1].OperatorAddress, validators[2].OperatorAddress}
	ballot := makeTypedBallot(t, "finalized-tss-ballot", uvalidatortypes.BallotObservationType_BALLOT_OBSERVATION_TYPE_TSS_KEY, voterStrs, nil, 3)
	ballot.Status = uvalidatortypes.BallotStatus_BALLOT_STATUS_PASSED
	require.NoError(t, chainApp.UvalidatorKeeper.Ballots.Set(ctx, ballot.Id, ballot))

	_, _, _, _, _, err := chainApp.UvalidatorKeeper.RecomputeBallotQuorum(ctx, ballot.Id)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not pending")
}

// The zero-eligible → EXPIRED path is reached only by allow-listed types; a
// refused type is refused outright and is NOT auto-expired as a side effect.
func TestRecomputeBallotQuorum_ZeroEligible_AllowedExpires_RefusedDoesNot(t *testing.T) {
	t.Run("allowed type still auto-expires", func(t *testing.T) {
		chainApp, ctx, validators := setupQueryTest(t, 3)
		for _, v := range validators {
			setUVStatus(t, chainApp, ctx, v, uvalidatortypes.UVStatus_UV_STATUS_ACTIVE)
		}

		voterStrs := []string{validators[0].OperatorAddress, validators[1].OperatorAddress, validators[2].OperatorAddress}
		ballot := makeTypedBallot(t, "zero-eligible-fund-migration", uvalidatortypes.BallotObservationType_BALLOT_OBSERVATION_TYPE_FUND_MIGRATION, voterStrs, nil, 3)
		require.NoError(t, chainApp.UvalidatorKeeper.Ballots.Set(ctx, ballot.Id, ballot))
		require.NoError(t, chainApp.UvalidatorKeeper.ActiveBallotIDs.Set(ctx, ballot.Id))

		for _, v := range validators {
			v.Status = stakingtypes.Unbonded
			require.NoError(t, chainApp.StakingKeeper.SetValidator(ctx, v))
		}

		_, newEligible, _, _, newStatus, err := chainApp.UvalidatorKeeper.RecomputeBallotQuorum(ctx, ballot.Id)
		require.NoError(t, err)
		require.Equal(t, int64(0), newEligible)
		require.Equal(t, uvalidatortypes.BallotStatus_BALLOT_STATUS_EXPIRED, newStatus)

		updated, _ := chainApp.UvalidatorKeeper.Ballots.Get(ctx, ballot.Id)
		require.Equal(t, uvalidatortypes.BallotStatus_BALLOT_STATUS_EXPIRED, updated.Status)
	})

	t.Run("refused type stays pending", func(t *testing.T) {
		chainApp, ctx, validators := setupQueryTest(t, 3)
		for _, v := range validators {
			setUVStatus(t, chainApp, ctx, v, uvalidatortypes.UVStatus_UV_STATUS_ACTIVE)
		}

		voterStrs := []string{validators[0].OperatorAddress, validators[1].OperatorAddress, validators[2].OperatorAddress}
		ballot := makeTypedBallot(t, "zero-eligible-tss", uvalidatortypes.BallotObservationType_BALLOT_OBSERVATION_TYPE_TSS_KEY, voterStrs, nil, 3)
		require.NoError(t, chainApp.UvalidatorKeeper.Ballots.Set(ctx, ballot.Id, ballot))
		require.NoError(t, chainApp.UvalidatorKeeper.ActiveBallotIDs.Set(ctx, ballot.Id))

		for _, v := range validators {
			v.Status = stakingtypes.Unbonded
			require.NoError(t, chainApp.StakingKeeper.SetValidator(ctx, v))
		}

		_, _, _, _, _, recomputeErr := chainApp.UvalidatorKeeper.RecomputeBallotQuorum(ctx, ballot.Id)

		after, getErr := chainApp.UvalidatorKeeper.Ballots.Get(ctx, ballot.Id)
		require.NoError(t, getErr)
		require.Equal(t, uvalidatortypes.BallotStatus_BALLOT_STATUS_PENDING, after.Status,
			"a refused recompute must not expire the ballot as a side effect")
		require.Equal(t, voterStrs, after.EligibleVoters)
		require.Equal(t, int64(3), after.VotingThreshold)

		require.Error(t, recomputeErr)
	})
}
