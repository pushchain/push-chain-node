package types

import (
	"context"

	uvalidatortypes "github.com/pushchain/push-chain-node/x/uvalidator/types"
)

// Uvalidator keeper
type UValidatorKeeper interface {
	IsTombstonedUniversalValidator(ctx context.Context, universalValidator string) (bool, error)
	IsBondedUniversalValidator(ctx context.Context, universalValidator string) (bool, error)
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
	GetEligibleVoters(ctx context.Context) ([]uvalidatortypes.UniversalValidator, error)
	GetAllUniversalValidators(ctx context.Context) ([]uvalidatortypes.UniversalValidator, error)
}
