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
func (b *Ballot) AddVote(address string, vote VoteResult, reason string) error {
	if b.Status != BallotStatus_BALLOT_STATUS_PENDING {
		return fmt.Errorf("cannot vote on ballot %s: not pending", b.Id)
	}

	idx := b.GetVoterIndex(address)
	if idx == -1 {
		return fmt.Errorf("voter %s not eligible", address)
	}

	if b.HasVoted(address) {
		return fmt.Errorf("voter %s already voted", address)
	}

	b.Votes[idx] = vote
	b.Reasons[idx] = reason
	return nil
}

// CountVotes counts the YES and NO votes in the ballot.
func (b Ballot) CountVotes() (yes, no int) {
	for _, v := range b.Votes {
		switch v {
		case VoteResult_VOTE_RESULT_YES:
			yes++
		case VoteResult_VOTE_RESULT_NO:
			no++
		}
	}
	return yes, no
}

// InitEmptyVotes initializes the Votes and Reasons slices to match EligibleVoters length.
func (b *Ballot) InitEmptyVotes() {
	n := len(b.EligibleVoters)
	b.Votes = make([]VoteResult, n)
	b.Reasons = make([]string, n)
	for i := 0; i < n; i++ {
		b.Votes[i] = VoteResult_VOTE_RESULT_NOT_YET_VOTED
		b.Reasons[i] = ""
	}
}

// IsExpired checks if the ballot has passed its expiry height.
func (b Ballot) IsExpired(currentHeight int64) bool {
	return currentHeight >= b.BlockHeightExpiry
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
