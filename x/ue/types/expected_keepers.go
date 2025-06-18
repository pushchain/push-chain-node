package types

import (
	"context"
	"math/big"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/evmos/os/x/evm/statedb"
	"github.com/evmos/os/x/evm/types"
)

// EVMKeeper defines the expected interface for the EVM module.
type EVMKeeper interface {
	CallEVMWithData(
		ctx sdk.Context,
		from common.Address,
		contract *common.Address,
		data []byte,
		commit bool,
	) (*types.MsgEthereumTxResponse, error)
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

// UtvKeeper defines the expected interface for the UTV module.
type UtvKeeper interface {
	VerifyGatewayInteractionTx(ctx context.Context, ownerKey, txHash, chainId string) error
	VerifyAndGetLockedFunds(ctx context.Context, ownerKey, txHash, chainId string) (big.Int, uint32, error)
}

// ParamSubspace defines the expected Subspace interface for parameters.
type ParamSubspace interface {
	Get(context.Context, []byte, interface{})
	Set(context.Context, []byte, interface{})
}
