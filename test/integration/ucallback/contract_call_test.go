package integrationtest

import (
	"math/big"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/app"
	utils "github.com/pushchain/push-chain-node/test/utils"
	ucallbacktypes "github.com/pushchain/push-chain-node/x/ucallback/types"
)

// The deployed contract must be the one we think it is: same reserved address, and
// the access-control immutable baked to the x/ucallback module account.
//
// A unit test asserts our ABI encoding; only this asserts the bytecode agrees.
func TestUniversalCallback_DeploysWithOurModuleAddress(t *testing.T) {
	chainApp, ctx, _ := utils.SetAppWithValidators(t)

	addr := utils.SetupUniversalCallback(t, chainApp, ctx)
	code := chainApp.EVMKeeper.GetCode(ctx, common.BytesToHash(
		chainApp.EVMKeeper.GetAccountOrEmpty(ctx, addr).CodeHash))
	require.NotEmpty(t, code, "the contract must have code at its reserved address")

	modAddr, _ := chainApp.UcallbackKeeper.GetModuleAddress(ctx)
	require.Contains(t, common.Bytes2Hex(code), common.Bytes2Hex(modAddr.Bytes()),
		"the module address must be baked into the deployed code, or every call reverts")
}

// The real contract's access control must admit our module and nothing else. The
// contract used to hardcode x/uexecutor's address; only a real call can show that
// the pairing is now with us.
func TestUniversalCallback_RejectsNonModuleCallers(t *testing.T) {
	chainApp, ctx, _ := utils.SetAppWithValidators(t)
	addr := utils.SetupUniversalCallback(t, chainApp, ctx)

	callbackABI, err := ucallbacktypes.ParseUniversalCallbackABI()
	require.NoError(t, err)

	stranger := provisionEOA(t, chainApp, ctx, "0x000000000000000000000000000000000000dEaD")
	res, callErr := call(t, chainApp, ctx, callbackABI, stranger, addr,
		false /* isModuleSender */, nil /* manualNonce: module senders only */)

	// A revert yields BOTH a response and an error — see ClassifyCall's ordering.
	require.Error(t, callErr, "onlyUCallbackModule must reject a stranger")
	require.NotNil(t, res, "a revert still returns the response carrying the reason")
	require.NotEmpty(t, res.VmError)

	require.Equal(t, ucallbacktypes.CallUnsettled,
		ucallbacktypes.ClassifyCall(res.VmError, res.Ret, callErr),
		"a rejected caller settled nothing, so the read must stay expirable")
}

// Our module reaches past access control. The call still reverts — the request does
// not exist — but on a different error, which is the point: the caller is admitted
// and the contract is evaluating the request itself.
func TestUniversalCallback_AdmitsTheModule(t *testing.T) {
	chainApp, ctx, _ := utils.SetAppWithValidators(t)
	addr := utils.SetupUniversalCallback(t, chainApp, ctx)

	callbackABI, err := ucallbacktypes.ParseUniversalCallbackABI()
	require.NoError(t, err)
	modAddr, _ := chainApp.UcallbackKeeper.GetModuleAddress(ctx)

	res, callErr := call(t, chainApp, ctx, callbackABI, modAddr, addr, true, nonceArg())
	require.Error(t, callErr, "an unknown request still reverts")
	require.NotNil(t, res)

	stranger := provisionEOA(t, chainApp, ctx, "0x000000000000000000000000000000000000dEaD")
	strangerRes, _ := call(t, chainApp, ctx, callbackABI, stranger, addr, false, nil)
	require.NotNil(t, strangerRes)

	require.NotEqual(t, common.Bytes2Hex(strangerRes.Ret), common.Bytes2Hex(res.Ret),
		"the module must fail for a different reason than a rejected caller")

	// and specifically: not the access-control error
	require.Equal(t, ucallbacktypes.CallerIsNotUCallbackModuleSelector(),
		selectorOf(strangerRes.Ret), "stranger is refused by access control")
	require.NotEqual(t, ucallbacktypes.CallerIsNotUCallbackModuleSelector(),
		selectorOf(res.Ret), "the module must get past access control")
}

// call issues expireExternalRead the way production does — via
// DerivedEVMCallWithData, which preserves the response on a revert. The ABI-typed
// DerivedEVMCall wrapper returns (nil, err) instead, discarding res.Ret; a test
// routed through it could not see which error the contract raised.
func call(
	t *testing.T, chainApp *app.ChainApp, ctx sdk.Context, callbackABI abi.ABI,
	from, contract common.Address, isModuleSender bool, nonce *uint64,
) (*evmtypes.MsgEthereumTxResponse, error) {
	t.Helper()
	data, err := callbackABI.Pack(ucallbacktypes.MethodExpireExternalRead, requestIDArg())
	require.NoError(t, err)
	return chainApp.EVMKeeper.DerivedEVMCallWithData(
		ctx, from, &contract, data,
		true /* commit */, false /* gasless */, isModuleSender,
		big.NewInt(0), gasLimitArg(), nonce,
	)
}

func selectorOf(ret []byte) [4]byte {
	var s [4]byte
	if len(ret) >= 4 {
		copy(s[:], ret[:4])
	}
	return s
}

// requestIDArg is an arbitrary uint256 request id for calls expected to revert
// before the id matters.
func requestIDArg() *big.Int { return big.NewInt(0xaa) }

func nonceArg() *uint64 { n := uint64(0); return &n }

// gasLimitArg is comfortably above intrinsic cost. Passing nil makes DerivedEVMCall
// estimate, and an estimate below intrinsic gas is rejected before the EVM runs —
// which would tell us nothing about the contract.
func gasLimitArg() *big.Int { return big.NewInt(500_000) }

// provisionEOA creates an account for a bare address. A non-module sender has its
// nonce read from the account keeper, which errors when the account does not exist —
// the call would fail before the EVM, telling us nothing about access control.
func provisionEOA(t *testing.T, chainApp *app.ChainApp, ctx sdk.Context, hex string) common.Address {
	t.Helper()
	addr := common.HexToAddress(hex)
	acc := chainApp.AccountKeeper.NewAccountWithAddress(ctx, sdk.AccAddress(addr.Bytes()))
	chainApp.AccountKeeper.SetAccount(ctx, acc)
	return addr
}
