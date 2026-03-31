package types

import (
	"context"
	"math/big"

	sdk "github.com/cosmos/cosmos-sdk/types"
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
	UpdateValidatorStatus(ctx context.Context, addr sdk.ValAddress, newStatus uvalidatortypes.UVStatus) error
}

// URegistryKeeper defines the expected interface for the uregistry keeper.
type URegistryKeeper interface {
	IsChainOutboundEnabled(ctx context.Context, chain string) (bool, error)
}

// UExecutorKeeper defines the expected interface for the uexecutor keeper.
type UExecutorKeeper interface {
	HasPendingOutboundsForChain(ctx context.Context, chain string) (bool, error)
	GetGasPriceByChain(ctx sdk.Context, chainNamespace string) (*big.Int, error)
}
