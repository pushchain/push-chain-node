package types

import (
	"context"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"

	sdk "github.com/cosmos/cosmos-sdk/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"

	uvalidatortypes "github.com/pushchain/push-chain-node/x/uvalidator/types"
)

// UValidatorKeeper is the slice of x/uvalidator that x/ucallback needs: the voter
// set and ballot primitive for read voting, the two eligibility checks, and the
// admin address that gates the expiry escape hatch.
type UValidatorKeeper interface {
	IsBondedUniversalValidator(ctx context.Context, universalValidator string) (bool, error)
	IsTombstonedUniversalValidator(ctx context.Context, universalValidator string) (bool, error)
	GetEligibleVoters(ctx context.Context) ([]uvalidatortypes.UniversalValidator, error)
	GetAdmin(ctx context.Context) (string, error)
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

// EVMKeeper is the slice of x/vm needed to call UniversalCallback. Only the
// derived-call entry point is required — reads never deploy or write state
// directly.
type EVMKeeper interface {
	DerivedEVMCall(
		ctx sdk.Context,
		abi abi.ABI,
		from, contract common.Address,
		value, gasLimit *big.Int,
		commit, gasless, isModuleSender bool,
		manualNonce *uint64,
		method string,
		args ...interface{},
	) (*evmtypes.MsgEthereumTxResponse, error)
}

// AccountKeeper resolves the x/ucallback module account, whose address is the
// caller UniversalCallback's access control admits.
type AccountKeeper interface {
	GetModuleAccount(ctx context.Context, moduleName string) sdk.ModuleAccountI
}
