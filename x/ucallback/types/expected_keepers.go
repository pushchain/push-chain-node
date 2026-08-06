package types

import (
	"context"

	uvalidatortypes "github.com/pushchain/push-chain-node/x/uvalidator/types"
)

// UValidatorKeeper is the slice of x/uvalidator that x/ucallback needs to run a
// read-result ballot. Narrower than x/uexecutor's equivalent — read voting needs
// the voter set, the ballot primitive, and the two eligibility checks, nothing more.
type UValidatorKeeper interface {
	IsBondedUniversalValidator(ctx context.Context, universalValidator string) (bool, error)
	IsTombstonedUniversalValidator(ctx context.Context, universalValidator string) (bool, error)
	GetEligibleVoters(ctx context.Context) ([]uvalidatortypes.UniversalValidator, error)
	VoteOnBallot(
		ctx context.Context,
		id string,
		ballotType uvalidatortypes.BallotObservationType,
		voter string,
		voteResult uvalidatortypes.VoteResult,
		voters []string,
		votesNeeded int64,
		expiryAfterBlocks int64,
	) (
		ballot uvalidatortypes.Ballot,
		isFinalized bool,
		isNew bool,
		err error)
}
