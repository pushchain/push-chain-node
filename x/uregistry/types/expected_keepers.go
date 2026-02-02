package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/evm/x/vm/statedb"
	"github.com/ethereum/go-ethereum/common"
)

// EVMKeeper defines the expected interface for the EVM module.
type EVMKeeper interface {
	GetAccount(_ sdk.Context, addr common.Address) *statedb.Account
	SetAccount(ctx sdk.Context, addr common.Address, account statedb.Account) error
	SetState(ctx sdk.Context, addr common.Address, key common.Hash, value []byte)
	GetCode(_ sdk.Context, codeHash common.Hash) []byte
	SetCode(ctx sdk.Context, codeHash, code []byte)
}
