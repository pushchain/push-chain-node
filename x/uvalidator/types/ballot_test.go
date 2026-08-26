package types

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func sampleBallot() Ballot {
	return NewBallot(
		"test-ballot",
		BallotObservationType_BALLOT_OBSERVATION_TYPE_UNSPECIFIED,
		[]string{"addr1", "addr2", "addr3"},
		2,   // threshold
		100, // created height
		50,  // expiry after blocks
	)
}

func TestGetVoterIndex(t *testing.T) {
	b := sampleBallot()
	require.Equal(t, 0, b.GetVoterIndex("addr1"))
	require.Equal(t, 1, b.GetVoterIndex("addr2"))
	require.Equal(t, -1, b.GetVoterIndex("addrX"))

	// Edge: empty voters
	empty := Ballot{}
	require.Equal(t, -1, empty.GetVoterIndex("addr1"))
}

func TestHasVoted(t *testing.T) {
	b := sampleBallot()
	require.False(t, b.HasVoted("addr1"))
	b.Votes[0] = VoteResult_VOTE_RESULT_SUCCESS
	require.True(t, b.HasVoted("addr1"))
	require.False(t, b.HasVoted("addrX"))
}

func TestAddVote(t *testing.T) {
	b := sampleBallot()

	// Valid vote
	b, err := b.AddVote("addr1", VoteResult_VOTE_RESULT_SUCCESS)
	require.NoError(t, err)
	require.True(t, b.HasVoted("addr1"))

	// Duplicate vote
	_, err = b.AddVote("addr1", VoteResult_VOTE_RESULT_FAILURE)
	require.Error(t, err)

	// Ineligible voter
	_, err = b.AddVote("addrX", VoteResult_VOTE_RESULT_SUCCESS)
	require.Error(t, err)

	// Ballot not pending
	b.Status = BallotStatus_BALLOT_STATUS_EXPIRED
	_, err = b.AddVote("addr2", VoteResult_VOTE_RESULT_SUCCESS)
	require.Error(t, err)

	// Edge: voting NO
	b = sampleBallot()
	b, err = b.AddVote("addr2", VoteResult_VOTE_RESULT_FAILURE)
	require.NoError(t, err)
	require.True(t, b.HasVoted("addr2"))
}

func TestCountVotes(t *testing.T) {
	b := sampleBallot()
	b.Votes[0] = VoteResult_VOTE_RESULT_SUCCESS
	b.Votes[1] = VoteResult_VOTE_RESULT_FAILURE
	yes, no := b.CountVotes()
	require.Equal(t, 1, yes)
	require.Equal(t, 1, no)

	// Edge: no votes
	b = sampleBallot()
	yes, no = b.CountVotes()
	require.Equal(t, 0, yes)
	require.Equal(t, 0, no)
}

func TestShouldPassAndReject(t *testing.T) {
	b := sampleBallot()

	// Not enough votes yet
	require.False(t, b.ShouldPass())
	require.False(t, b.ShouldReject())

	// Pass condition
	b.Votes[0] = VoteResult_VOTE_RESULT_SUCCESS
	b.Votes[1] = VoteResult_VOTE_RESULT_SUCCESS
	require.True(t, b.ShouldPass())

	// Reject condition
	b = sampleBallot()
	b.Votes[0] = VoteResult_VOTE_RESULT_FAILURE
	b.Votes[1] = VoteResult_VOTE_RESULT_FAILURE
	require.True(t, b.ShouldReject())

	// Edge: threshold larger than voters
	b = NewBallot("big-threshold", BallotObservationType_BALLOT_OBSERVATION_TYPE_UNSPECIFIED, []string{"a"}, 5, 1, 10)
	require.False(t, b.ShouldPass())
	require.True(t, b.ShouldReject())
}

func TestIsExpiredRemainingTimeAge(t *testing.T) {
	b := sampleBallot()

	// Not expired
	require.False(t, b.IsExpired(120))
	require.Equal(t, int64(30), b.RemainingTime(120))

	// Exactly at expiry
	require.True(t, b.IsExpired(150))
	require.Equal(t, int64(0), b.RemainingTime(150))

	// Expired beyond expiry
	require.True(t, b.IsExpired(200))
	require.Equal(t, int64(0), b.RemainingTime(200))

	// Age
	require.Equal(t, int64(20), b.Age(120))
}

func TestInitEmptyVotes(t *testing.T) {
	b := Ballot{EligibleVoters: []string{"a", "b"}}
	b.InitEmptyVotes()
	require.Len(t, b.Votes, 2)
	for _, v := range b.Votes {
		require.Equal(t, VoteResult_VOTE_RESULT_NOT_YET_VOTED, v)
	}

	// Edge: no voters
	b = Ballot{}
	b.InitEmptyVotes()
	require.Empty(t, b.Votes)
}

func TestExpiryTime(t *testing.T) {
	b := sampleBallot()
	avgBlockTime := time.Second * 5
	expiry := b.ExpiryTime(avgBlockTime)
	require.WithinDuration(t, time.Now().Add(avgBlockTime*50), expiry, time.Second*1)

	// Edge: expiryAfterBlocks = 0
	b = NewBallot("instant-expiry", BallotObservationType_BALLOT_OBSERVATION_TYPE_UNSPECIFIED, []string{"a"}, 1, 100, 0)
	expiry = b.ExpiryTime(avgBlockTime)
	require.WithinDuration(t, time.Now(), expiry, time.Second*1)
}

func TestIsFinalized(t *testing.T) {
	b := sampleBallot()
	require.False(t, b.IsFinalized())

	b.Status = BallotStatus_BALLOT_STATUS_PASSED
	require.True(t, b.IsFinalized())

	b.Status = BallotStatus_BALLOT_STATUS_REJECTED
	require.True(t, b.IsFinalized())
}

func TestIsFinalizingVote(t *testing.T) {
	b := sampleBallot()

	// Not enough votes yet
	updated, done := b.IsFinalizingVote()
	require.False(t, done)
	require.Equal(t, BallotStatus_BALLOT_STATUS_PENDING, updated.Status)

	// Passing votes reach threshold
	b.Votes[0] = VoteResult_VOTE_RESULT_SUCCESS
	b.Votes[1] = VoteResult_VOTE_RESULT_SUCCESS
	updated, done = b.IsFinalizingVote()
	require.True(t, done)
	require.Equal(t, BallotStatus_BALLOT_STATUS_PASSED, updated.Status)

	// Rejecting votes reach threshold
	b = sampleBallot()
	b.Votes[0] = VoteResult_VOTE_RESULT_FAILURE
	b.Votes[1] = VoteResult_VOTE_RESULT_FAILURE
	updated, done = b.IsFinalizingVote()
	require.True(t, done)
	require.Equal(t, BallotStatus_BALLOT_STATUS_REJECTED, updated.Status)

	// Already finalized ballot
	b.Status = BallotStatus_BALLOT_STATUS_EXPIRED
	_, done = b.IsFinalizingVote()
	require.False(t, done)
}

// TestIsUnreachablePending pins the F-2026-18147 predicate: a PENDING ballot
// whose every eligible voter has already voted can never receive another vote
// (AddVote rejects repeats), so it is terminal in fact.
func TestIsUnreachablePending(t *testing.T) {
	voted := func(v ...VoteResult) Ballot {
		return Ballot{
			Id:              "b",
			Status:          BallotStatus_BALLOT_STATUS_PENDING,
			EligibleVoters:  []string{"addr1", "addr2", "addr3"}[:len(v)],
			Votes:           v,
			VotingThreshold: 2,
		}
	}

	// Every slot filled → unreachable, regardless of the vote arithmetic.
	require.True(t, voted(
		VoteResult_VOTE_RESULT_SUCCESS,
		VoteResult_VOTE_RESULT_SUCCESS,
		VoteResult_VOTE_RESULT_SUCCESS,
	).IsUnreachablePending(), "YES above threshold but nobody left to vote")
	require.True(t, voted(
		VoteResult_VOTE_RESULT_SUCCESS,
		VoteResult_VOTE_RESULT_FAILURE,
		VoteResult_VOTE_RESULT_FAILURE,
	).IsUnreachablePending(), "YES below threshold and nobody left to vote")

	// One slot still open → the ballot can still finalize normally.
	require.False(t, voted(
		VoteResult_VOTE_RESULT_SUCCESS,
		VoteResult_VOTE_RESULT_SUCCESS,
		VoteResult_VOTE_RESULT_NOT_YET_VOTED,
	).IsUnreachablePending())

	// Only PENDING ballots qualify; terminal ones are fully voted by
	// construction and must not be swept in.
	for _, st := range []BallotStatus{
		BallotStatus_BALLOT_STATUS_PASSED,
		BallotStatus_BALLOT_STATUS_REJECTED,
		BallotStatus_BALLOT_STATUS_EXPIRED,
	} {
		b := voted(VoteResult_VOTE_RESULT_SUCCESS, VoteResult_VOTE_RESULT_SUCCESS)
		b.Status = st
		require.False(t, b.IsUnreachablePending(), "status %s must not be reported as unreachable-pending", st)
	}

	// Degenerate ballots are refused rather than swept in: an empty voter list
	// is a malformed ballot, and RecomputeBallotQuorum resolves it by rebuilding
	// the list from the live UV set (or auto-expiring at zero).
	require.False(t, Ballot{Status: BallotStatus_BALLOT_STATUS_PENDING}.IsUnreachablePending())

	// A Votes slice shorter than EligibleVoters is malformed too; the missing
	// slots count as unvoted, which is the conservative answer.
	short := Ballot{
		Status:         BallotStatus_BALLOT_STATUS_PENDING,
		EligibleVoters: []string{"addr1", "addr2"},
		Votes:          []VoteResult{VoteResult_VOTE_RESULT_SUCCESS},
	}
	require.True(t, short.HasUnvotedEligibleVoter())
	require.False(t, short.IsUnreachablePending())
}
