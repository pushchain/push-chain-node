package types

import (
	"context"
	"math/big"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/evm/x/vm/statedb"
	"github.com/cosmos/evm/x/vm/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	uregistrytypes "github.com/rollchains/pchain/x/uregistry/types"
	uvalidatortypes "github.com/rollchains/pchain/x/uvalidator/types"
)

// UregistryKeeper defines the expected interface for the UE module.
type UregistryKeeper interface {
	GetChainConfig(ctx context.Context, chain string) (uregistrytypes.ChainConfig, error)
	IsChainOutboundEnabled(ctx context.Context, chain string) (bool, error)
	IsChainInboundEnabled(ctx context.Context, chain string) (bool, error)
}

// EVMKeeper defines the expected interface for the EVM module.
type EVMKeeper interface {
	CallEVM(
		ctx sdk.Context,
		abi abi.ABI,
		from, contract common.Address,
		commit bool,
		method string,
		args ...interface{},
	) (*types.MsgEthereumTxResponse, error)
	SetAccount(ctx sdk.Context, addr common.Address, account statedb.Account) error
	SetState(ctx sdk.Context, addr common.Address, key common.Hash, value []byte)
	SetCode(ctx sdk.Context, codeHash, code []byte)
	DerivedEVMCall(
		ctx sdk.Context,
		abi abi.ABI,
		from, contract common.Address,
		value, gasLimit *big.Int,
		commit, gasless bool,
		method string,
		args ...interface{},
	) (*types.MsgEthereumTxResponse, error)
}

// FeeMarketKeeper defines the expected interface for the fee market module.
type FeeMarketKeeper interface {
	GetBaseFee(ctx sdk.Context) math.LegacyDec
}

// BankKeeper defines the expected interface for the bank module.
type BankKeeper interface {
	SendCoinsFromAccountToModule(
		ctx context.Context,
		senderAddr sdk.AccAddress,
		recipientModule string,
		amt sdk.Coins,
	) error

	SendCoinsFromModuleToAccount(
		ctx context.Context,
		senderModule string,
		recipientAddr sdk.AccAddress,
		amt sdk.Coins,
	) error

	BurnCoins(
		ctx context.Context,
		moduleName string,
		amt sdk.Coins,
	) error

	MintCoins(
		ctx context.Context,
		moduleName string,
		amt sdk.Coins,
	) error
}

// AccountKeeper defines the expected interface for the auth module
type AccountKeeper interface {
	GetModuleAccount(ctx context.Context, moduleName string) sdk.ModuleAccountI
}

// UtvKeeper defines the expected interface for the UTV module.
type UtvKeeper interface {
	VerifyGatewayInteractionTx(ctx context.Context, ownerKey, txHash, chain string) error
	VerifyAndGetLockedFunds(ctx context.Context, ownerKey, txHash, chain string) (big.Int, uint32, error)
}

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
	GetUniversalValidatorSet(ctx context.Context) ([]string, error)
}

// ParamSubspace defines the expected Subspace interface for parameters.
type ParamSubspace interface {
	Get(context.Context, []byte, interface{})
	Set(context.Context, []byte, interface{})
}
