package types

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
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
}

// ParamSubspace defines the expected Subspace interface for parameters.
type ParamSubspace interface {
	Get(context.Context, []byte, interface{})
	Set(context.Context, []byte, interface{})
}
