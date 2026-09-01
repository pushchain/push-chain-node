package types

import (
	"context"
	"math/big"

	"github.com/ethereum/go-ethereum/common"

	"cosmossdk.io/math"
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
//
// DerivedEVMCallWithData, and deliberately NOT the ABI-typed DerivedEVMCall
// wrapper that sits above it. On a revert that wrapper returns (nil, err),
// discarding the response and with it res.Ret — the revert data ClassifyCall reads
// to tell "already settled" from "try again". Leaving it off this interface makes
// reaching for it a compile error rather than something a reviewer has to catch.
type EVMKeeper interface {
	DerivedEVMCallWithData(
		ctx sdk.Context,
		from common.Address,
		contract *common.Address,
		data []byte,
		commit, gasless, isModuleSender bool,
		value, gasLimit *big.Int,
		manualNonce *uint64,
	) (*evmtypes.MsgEthereumTxResponse, error)
}

// AccountKeeper resolves the x/ucallback module account, whose address is the
// caller UniversalCallback's access control admits.
type AccountKeeper interface {
	GetModuleAccount(ctx context.Context, moduleName string) sdk.ModuleAccountI
}

// FeeMarketKeeper supplies the base fee used to price callback gas. Same source
// x/uexecutor uses in CalculateGasCost, so a read and a UEA execution are valued
// identically.
//
// Satisfied by a value, not a pointer to the app field: the keeper is constructed
// after x/feemarket, so there is nothing left to populate.
type FeeMarketKeeper interface {
	GetBaseFee(ctx sdk.Context) math.LegacyDec
}

// BankKeeper covers moving the consumed callback budget out of the contract and
// destroying it.
//
// The contract deliberately leaves itself over-collateralised by the burned amount
// after reportCallbackGas, expecting the module to take it. A contract's balance is
// an ordinary bank balance, so this needs no contract-side API — the same shape as
// x/uexecutor's DeductAndBurnFees.
type BankKeeper interface {
	SendCoinsFromAccountToModule(ctx context.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error
	BurnCoins(ctx context.Context, moduleName string, amt sdk.Coins) error
	GetBalance(ctx context.Context, addr sdk.AccAddress, denom string) sdk.Coin
}
