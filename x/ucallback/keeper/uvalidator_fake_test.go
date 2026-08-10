package keeper_test

import (
	"context"
	"fmt"

	"github.com/pushchain/push-chain-node/x/ucallback/types"
	uvalidatortypes "github.com/pushchain/push-chain-node/x/uvalidator/types"
)

// fakeUValidator is an in-memory stand-in for x/uvalidator.
//
// It tallies votes for real rather than returning a canned isFinalized, because
// the behaviour under test is precisely that two validators reporting different
// observations land on different ballots and neither reaches quorum. A stub that
// ignored the ballot key could not distinguish that from success.
type fakeUValidator struct {
	voters     []string
	bonded     map[string]bool
	tombstoned map[string]bool
	admin      string

	ballots map[string]*fakeBallot

	// set to force an error out of the corresponding call
	votersErr error
	voteErr   error
}

type fakeBallot struct {
	observationType uvalidatortypes.BallotObservationType
	votes           map[string]uvalidatortypes.VoteResult
	finalized       bool
	expiryBlocks    int64
}

var _ types.UValidatorKeeper = (*fakeUValidator)(nil)

func newFakeUValidator(voters ...string) *fakeUValidator {
	f := &fakeUValidator{
		voters:     voters,
		bonded:     map[string]bool{},
		tombstoned: map[string]bool{},
		ballots:    map[string]*fakeBallot{},
	}
	for _, v := range voters {
		f.bonded[v] = true
	}
	f.admin = "push1adminadminadminadminadminadminadmin"
	return f
}

func (f *fakeUValidator) GetAdmin(context.Context) (string, error) {
	return f.admin, nil
}

func (f *fakeUValidator) IsBondedUniversalValidator(_ context.Context, v string) (bool, error) {
	return f.bonded[v], nil
}

func (f *fakeUValidator) IsTombstonedUniversalValidator(_ context.Context, v string) (bool, error) {
	return f.tombstoned[v], nil
}

func (f *fakeUValidator) GetEligibleVoters(_ context.Context) ([]uvalidatortypes.UniversalValidator, error) {
	if f.votersErr != nil {
		return nil, f.votersErr
	}
	out := make([]uvalidatortypes.UniversalValidator, 0, len(f.voters))
	for _, v := range f.voters {
		out = append(out, uvalidatortypes.UniversalValidator{
			IdentifyInfo: &uvalidatortypes.IdentityInfo{CoreValidatorAddress: v},
		})
	}
	return out, nil
}

func (f *fakeUValidator) VoteOnBallot(
	_ context.Context,
	id string,
	ballotType uvalidatortypes.BallotObservationType,
	voter string,
	voteResult uvalidatortypes.VoteResult,
	_ []string,
	votesNeeded int64,
	expiryAfterBlocks int64,
) (uvalidatortypes.Ballot, bool, bool, error) {
	if f.voteErr != nil {
		return uvalidatortypes.Ballot{}, false, false, f.voteErr
	}

	b, existed := f.ballots[id]
	if !existed {
		b = &fakeBallot{
			observationType: ballotType,
			votes:           map[string]uvalidatortypes.VoteResult{},
			expiryBlocks:    expiryAfterBlocks,
		}
		f.ballots[id] = b
	}

	if b.finalized {
		return uvalidatortypes.Ballot{Id: id}, true, false, nil
	}
	if _, dup := b.votes[voter]; dup {
		return uvalidatortypes.Ballot{Id: id}, false, false,
			fmt.Errorf("validator %s already voted on ballot %s", voter, id)
	}

	b.votes[voter] = voteResult
	b.finalized = int64(len(b.votes)) >= votesNeeded

	return uvalidatortypes.Ballot{Id: id}, b.finalized, !existed, nil
}

// ballotCount reports how many distinct ballots have been opened — the signal that
// validators diverged on what they observed.
func (f *fakeUValidator) ballotCount() int { return len(f.ballots) }

// expiryOf returns the relative expiry a ballot was created with.
func (f *fakeUValidator) expiryOf(id string) int64 {
	b, ok := f.ballots[id]
	if !ok {
		return -1
	}
	return b.expiryBlocks
}

// errTest is a sentinel for injecting failures into the fake.
var errTest = fmt.Errorf("injected test failure")
