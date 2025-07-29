package precompile_util

import (
	"fmt"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/evm/x/vm/core/vm"
)

const (
	ErrDifferentOrigin = "tx origin address %s does not match the sender address %s"
)

// ExecuteMsg is a helper function that handles the common pattern of executing a message
func ExecuteMsg(
	ctx sdk.Context,
	origin common.Address,
	contract *vm.Contract,
	_ vm.StateDB,
	method *abi.Method,
	args []interface{},
	msgCreator func([]interface{}) (interface{}, common.Address, error),
	msgHandler func(ctx sdk.Context, msg interface{}) error,
	errorPrefix string,
) ([]byte, error) {
	msg, signer, err := msgCreator(args)
	if err != nil {
		return nil, err
	}

	// If the contract is the executor, we don't need an origin check
	// Otherwise check if the origin matches the sender address
	isContractExec := contract.CallerAddress == signer && contract.CallerAddress != origin
	if !isContractExec && origin != signer {
		return nil, fmt.Errorf(ErrDifferentOrigin, origin.String(), signer.String())
	}

	if err = msgHandler(ctx, msg); err != nil {
		return nil, fmt.Errorf("%s: %s", errorPrefix, err)
	}

	return method.Outputs.Pack(true)
}
