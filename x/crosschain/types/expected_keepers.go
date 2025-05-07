package types

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
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
}

// BankKeeper defines the expected interface for the bank module.
type BankKeeper interface {
	SendCoinsFromAccountToModule(
		ctx context.Context,
		senderAddr sdk.AccAddress,
		recipientModule string,
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

// ParamSubspace defines the expected Subspace interface for parameters.
type ParamSubspace interface {
	Get(context.Context, []byte, interface{})
	Set(context.Context, []byte, interface{})
}
