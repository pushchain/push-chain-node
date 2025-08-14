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
}

func TestHasVoted(t *testing.T) {
	b := sampleBallot()
	require.False(t, b.HasVoted("addr1"))
	b.Votes[0] = VoteResult_VOTE_RESULT_YES
	require.True(t, b.HasVoted("addr1"))
	require.False(t, b.HasVoted("addrX"))
}

func TestAddVote(t *testing.T) {
	b := sampleBallot()

	// Valid vote
	err := b.AddVote("addr1", VoteResult_VOTE_RESULT_YES)
	require.NoError(t, err)
	require.True(t, b.HasVoted("addr1"))

	// Duplicate vote
	err = b.AddVote("addr1", VoteResult_VOTE_RESULT_NO)
	require.Error(t, err)

	// Ineligible voter
	err = b.AddVote("addrX", VoteResult_VOTE_RESULT_YES)
	require.Error(t, err)

	// Ballot not pending
	b.Status = BallotStatus_BALLOT_STATUS_EXPIRED
	err = b.AddVote("addr2", VoteResult_VOTE_RESULT_YES)
	require.Error(t, err)
}

func TestCountVotes(t *testing.T) {
	b := sampleBallot()
	b.Votes[0] = VoteResult_VOTE_RESULT_YES
	b.Votes[1] = VoteResult_VOTE_RESULT_NO
	yes, no := b.CountVotes()
	require.Equal(t, 1, yes)
	require.Equal(t, 1, no)
}

func TestShouldPassAndReject(t *testing.T) {
	b := sampleBallot()

	// Not enough votes yet
	require.False(t, b.ShouldPass())
	require.False(t, b.ShouldReject())

	// Pass condition
	b.Votes[0] = VoteResult_VOTE_RESULT_YES
	b.Votes[1] = VoteResult_VOTE_RESULT_YES
	require.True(t, b.ShouldPass())

	// Reject condition
	b = sampleBallot()
	b.Votes[0] = VoteResult_VOTE_RESULT_NO
	b.Votes[1] = VoteResult_VOTE_RESULT_NO
	require.True(t, b.ShouldReject())
}

func TestIsExpiredRemainingTimeAge(t *testing.T) {
	b := sampleBallot()

	// Not expired
	require.False(t, b.IsExpired(120))
	require.Equal(t, int64(30), b.RemainingTime(120))

	// Expired
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
}

func TestExpiryTime(t *testing.T) {
	b := sampleBallot()
	avgBlockTime := time.Second * 5
	expiry := b.ExpiryTime(avgBlockTime)
	require.WithinDuration(t, time.Now().Add(avgBlockTime*50), expiry, time.Second*1)
}
