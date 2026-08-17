package keeper_test

import (
	"context"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"

	"github.com/pushchain/push-chain-node/x/ucallback/types"
)

// recordedCall captures one DerivedEVMCall so tests can assert on what was sent to
// the contract, not merely that something was.
type recordedCall struct {
	from     common.Address
	contract common.Address
	method   string
	args     []interface{}
	nonce    *uint64
	gasLimit *big.Int
	isModule bool
}

type fakeEVM struct {
	calls []recordedCall

	// per-call outcomes, consumed in order; the zero value means success
	vmErrors []string
	// revertData is returned alongside every vmError — set it to a custom-error
	// selector to exercise the classification path.
	revertData []byte
	callErr    error
}

var _ types.EVMKeeper = (*fakeEVM)(nil)

func (f *fakeEVM) DerivedEVMCall(
	_ sdk.Context,
	_ abi.ABI,
	from, contract common.Address,
	_, gasLimit *big.Int,
	_, _, isModuleSender bool,
	manualNonce *uint64,
	method string,
	args ...interface{},
) (*evmtypes.MsgEthereumTxResponse, error) {
	var n *uint64
	if manualNonce != nil {
		v := *manualNonce
		n = &v
	}
	f.calls = append(f.calls, recordedCall{
		from: from, contract: contract, method: method, args: args,
		nonce: n, gasLimit: gasLimit, isModule: isModuleSender,
	})

	if f.callErr != nil {
		return nil, f.callErr
	}

	res := &evmtypes.MsgEthereumTxResponse{
		Hash:    "0xEVMTX",
		GasUsed: 21_000,
	}
	if len(f.vmErrors) > 0 {
		res.VmError = f.vmErrors[0]
		res.Ret = f.revertData
		f.vmErrors = f.vmErrors[1:]
	}
	return res, nil
}

func (f *fakeEVM) lastCall() recordedCall { return f.calls[len(f.calls)-1] }

// fakeAccount resolves the module account. The nonce is no longer faked — it lives
// in x/ucallback's own state now, so the tests exercise the real counter.
type fakeAccount struct {
	addr sdk.AccAddress
}

var _ types.AccountKeeper = (*fakeAccount)(nil)

func (f *fakeAccount) GetModuleAccount(context.Context, string) sdk.ModuleAccountI {
	return authtypes.NewEmptyModuleAccount(types.ModuleName)
}
