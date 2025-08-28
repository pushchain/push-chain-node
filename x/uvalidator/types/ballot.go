package types

import (
	"fmt"
	"time"
)

// GetVoterIndex returns the index of the given address in the eligible voter list.
// Returns -1 if not found.
func (b Ballot) GetVoterIndex(address string) int {
	for i, addr := range b.EligibleVoters {
		if addr == address {
			return i
		}
	}
	return -1
}

// HasVoted checks if the given voter address already has a vote recorded.
func (b Ballot) HasVoted(address string) bool {
	idx := b.GetVoterIndex(address)
	if idx == -1 {
		return false
	}
	return b.Votes[idx] != VoteResult_VOTE_RESULT_NOT_YET_VOTED
}

// AddVote records a vote for the given voter.
// Ensures the voter is eligible, hasn't already voted, and ballot is pending.
func (b Ballot) AddVote(address string, vote VoteResult) (Ballot, error) {
	if b.Status != BallotStatus_BALLOT_STATUS_PENDING {
		return b, fmt.Errorf("cannot vote on ballot %s: not pending", b.Id)
	}

	idx := b.GetVoterIndex(address)
	if idx == -1 {
		return b, fmt.Errorf("voter %s not eligible", address)
	}

	if b.HasVoted(address) {
		return b, fmt.Errorf("voter %s already voted", address)
	}

	b.Votes[idx] = vote
	return b, nil
}

// CountVotes counts the YES and NO votes in the ballot.
func (b Ballot) CountVotes() (yes, no int) {
	for _, v := range b.Votes {
		switch v {
		case VoteResult_VOTE_RESULT_SUCCESS:
			yes++
		case VoteResult_VOTE_RESULT_FAILURE:
			no++
		}
	}
	return yes, no
}

// InitEmptyVotes initializes the Votes slice to match EligibleVoters length.
func (b *Ballot) InitEmptyVotes() {
	n := len(b.EligibleVoters)
	b.Votes = make([]VoteResult, n)
	for i := 0; i < n; i++ {
		b.Votes[i] = VoteResult_VOTE_RESULT_NOT_YET_VOTED
	}
}

// IsExpired checks if the ballot has passed its expiry height.
func (b Ballot) IsExpired(currentHeight int64) bool {
	return currentHeight >= b.BlockHeightExpiry
}

func (b Ballot) IsFinalized() bool {
	return b.IsFinalized()
}

// ShouldPass returns true if the YES votes meet or exceed the stored voting threshold.
func (b Ballot) ShouldPass() bool {
	yesVotes, _ := b.CountVotes()
	return yesVotes >= int(b.VotingThreshold)
}

// ShouldReject returns true if the NO votes make it impossible to reach the threshold.
func (b Ballot) ShouldReject() bool {
	_, noVotes := b.CountVotes()
	potentialYesVotes := len(b.EligibleVoters) - noVotes
	return potentialYesVotes < int(b.VotingThreshold)
}

// NewBallot creates a new Ballot with default pending state.
func NewBallot(
	id string,
	ballotType BallotObservationType,
	voters []string,
	votesNeeded int64,
	createdHeight int64,
	expiryAfterBlocks int64,
) Ballot {
	b := Ballot{
		Id:                 id,
		BallotType:         ballotType,
		EligibleVoters:     voters,
		VotingThreshold:    votesNeeded,
		Status:             BallotStatus_BALLOT_STATUS_PENDING,
		BlockHeightCreated: createdHeight,
		BlockHeightExpiry:  createdHeight + expiryAfterBlocks,
	}
	b.InitEmptyVotes()
	return b
}

// RemainingTime returns the remaining time in blocks until expiry (useful for queries).
func (b Ballot) RemainingTime(currentHeight int64) int64 {
	if b.IsExpired(currentHeight) {
		return 0
	}
	return b.BlockHeightExpiry - currentHeight
}

// Age returns how many blocks have passed since creation.
func (b Ballot) Age(currentHeight int64) int64 {
	return currentHeight - b.BlockHeightCreated
}

// ExpiryTime estimates the wall-clock time at expiry given an average block time.
func (b Ballot) ExpiryTime(avgBlockTime time.Duration) time.Time {
	return time.Now().Add(avgBlockTime * time.Duration(b.BlockHeightExpiry-b.BlockHeightCreated))
}

// IsFinalizingVote checks if the ballot is reaching the finalization in this tx
func (b Ballot) IsFinalizingVote() (Ballot, bool) {
	// Only pending ballots can still be finalized
	if b.Status != BallotStatus_BALLOT_STATUS_PENDING {
		return b, false
	}

	// Count votes
	yesVotes := 0
	noVotes := 0
	for _, v := range b.Votes {
		switch v {
		case VoteResult_VOTE_RESULT_SUCCESS:
			yesVotes++
		case VoteResult_VOTE_RESULT_FAILURE:
			noVotes++
		}
	}

	// If YES or NO has reached/exceeded threshold → finalizing
	if int64(yesVotes) >= b.VotingThreshold {
		b.Status = BallotStatus_BALLOT_STATUS_PASSED
		return b, true
	}
	if int64(noVotes) >= b.VotingThreshold {
		b.Status = BallotStatus_BALLOT_STATUS_REJECTED
		return b, true
	}

	return b, false
}
