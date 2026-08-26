package integrationtest

import (
	"math/big"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/app"
	utils "github.com/pushchain/push-chain-node/test/utils"
	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
)

// F-2026-18189 — Manual Module EVM Nonce Desync on Reverted Inbound Execution.
//
// Every module-sender DerivedEVMCall in x/uexecutor supplies a *manual* nonce.
// x/vm turns that nonce into the derived transaction's identity:
//
//	ethtypes.NewTx(&DynamicFeeTx{Nonce, GasFeeCap, GasTipCap, Gas, To, Value, Data})
//	  -> tx.Hash() -> txConfig.TxHash -> res.Hash / the ethereum_tx event attribute
//
// so the nonce is the only field that distinguishes two otherwise byte-identical
// module calls. Two properties therefore have to hold at once, and these two tests
// pin one each:
//
//  1. the nonce the module hands to x/vm must not drift away from the module
//     account's own EVM nonce when an attempt fails (Hacken's reported defect), and
//  2. a nonce must be burned by every *attempt*, not just by every committed
//     success — otherwise a retry after a failed attempt reproduces a derived tx
//     hash that has already been emitted in this block.
//
// (2) is why the naive "read evm.GetNonce(module) immediately before each call and
// drop the counter" fix cannot be shipped: x/vm only advances a sender's nonce for
// a CREATE (state_transition.go bumps it in the contractCreation branch only), so
// for the plain CALLs the module makes, evm.GetNonce(module) is a constant and
// every byte-identical call would collide. The module has to advance that nonce
// itself, unconditionally.

// moduleDerivedTxHashes returns the ethereum_tx hashes emitted on ctx, in order.
// A derived tx that dies before execution (see the gas-estimation note in
// TestModuleSenderNonceDistinctHashesAcrossFailedAttempt) emits nothing, so this
// is also how the tests tell "attempted and emitted" from "attempted and dropped".
func moduleDerivedTxHashes(ctx sdk.Context) []string {
	var out []string
	for _, ev := range ctx.EventManager().Events() {
		if ev.Type != evmtypes.EventTypeEthereumTx {
			continue
		}
		for _, attr := range ev.Attributes {
			if attr.Key == evmtypes.AttributeKeyEthereumTxHash {
				out = append(out, attr.Value)
			}
		}
	}
	return out
}

// moduleNonceState reports the two values that must never diverge: the persisted
// uexecutor counter that feeds the manual nonce, and the module account's own EVM
// nonce that x/vm and eth_getTransactionCount read.
func moduleNonceState(t *testing.T, chainApp *app.ChainApp, ctx sdk.Context) (counter, evmNonce uint64) {
	t.Helper()

	counter, err := chainApp.UexecutorKeeper.GetModuleAccountNonce(ctx)
	require.NoError(t, err)

	moduleAddr, _ := chainApp.UexecutorKeeper.GetUeModuleAddress(ctx)
	return counter, chainApp.EVMKeeper.GetNonce(ctx, moduleAddr)
}

// callDepositPRC20 issues one module-sender depositPRC20Token through the real
// keeper entry point, on an isolated event manager so the caller sees only the
// ethereum_tx events this one call produced.
func callDepositPRC20(
	t *testing.T,
	chainApp *app.ChainApp,
	ctx sdk.Context,
	prc20, to common.Address,
	amount *big.Int,
) (hashes []string, err error) {
	t.Helper()

	callCtx := ctx.WithEventManager(sdk.NewEventManager())
	_, err = chainApp.UexecutorKeeper.CallPRC20Deposit(callCtx, prc20, to, amount)
	return moduleDerivedTxHashes(callCtx), err
}

// TestModuleSenderNonceSurvivesRevertedDeposit is Hacken's stated case: a
// depositPRC20Token that fails must not leave the module's nonce bookkeeping in a
// state that breaks the *next* module-sender call.
//
// The forced failure is a deposit of a PRC20 address that has no code. The
// UniversalCore handler makes a high-level call into it, which reverts.
func TestModuleSenderNonceSurvivesRevertedDeposit(t *testing.T) {
	chainApp, ctx, _ := utils.SetAppWithValidators(t)

	prc20 := utils.GetDefaultAddresses().PRC20USDCAddr
	// No contract is ever deployed here, so depositPRC20Token reverts on it.
	codelessPRC20 := common.HexToAddress("0x000000000000000000000000000000000000dEaD")
	recipient := common.HexToAddress("0x0000000000000000000000000000000000001234")
	amount := big.NewInt(1_000_000)

	// Sanity: the module account is the EVM sender for all of these calls.
	moduleAddr, _ := chainApp.UexecutorKeeper.GetUeModuleAddress(ctx)
	require.Equal(t,
		sdk.AccAddress(moduleAddr.Bytes()),
		chainApp.AccountKeeper.GetModuleAccount(ctx, uexecutortypes.ModuleName).GetAddress(),
	)

	counter, evmNonce := moduleNonceState(t, chainApp, ctx)
	require.Equal(t, counter, evmNonce, "module nonce must start in sync")

	// A committed success first, so the reverted attempt below is not the very
	// first thing the module ever does.
	_, err := callDepositPRC20(t, chainApp, ctx, prc20, recipient, amount)
	require.NoError(t, err, "baseline deposit must succeed")

	counter, evmNonce = moduleNonceState(t, chainApp, ctx)
	require.Equal(t, counter, evmNonce, "module nonce must stay in sync after a committed deposit")

	beforeRevertCounter, _ := moduleNonceState(t, chainApp, ctx)

	// The reverted deposit. The inbound executors swallow this error and return
	// nil, so on-chain nothing else reacts to it — whatever it leaves behind in
	// the nonce bookkeeping is what the next call has to live with.
	_, err = callDepositPRC20(t, chainApp, ctx, codelessPRC20, recipient, amount)
	require.Error(t, err, "depositing a codeless PRC20 must fail")

	afterRevertCounter, afterRevertEvmNonce := moduleNonceState(t, chainApp, ctx)

	// The next module-sender call — Hacken's reported impact is that this one is
	// blocked by the nonce the failed attempt left behind.
	_, err = callDepositPRC20(t, chainApp, ctx, prc20, recipient, amount)
	require.NoError(t, err, "a module-sender call after a reverted one must still succeed")

	// The failed attempt must still have consumed its nonce. A nonce handed back
	// on failure is a nonce a byte-identical retry can re-use, and the derived tx
	// hash is a pure function of the nonce and the calldata — see
	// TestModuleSenderNonceDistinctHashesAcrossFailedAttempt.
	require.Equal(t, beforeRevertCounter+1, afterRevertCounter,
		"a failed module-sender attempt must still burn its nonce")

	// ...and this is the drift itself: the counter that feeds the manual nonce
	// and the module account's own EVM nonce must still agree.
	require.Equal(t, afterRevertCounter, afterRevertEvmNonce,
		"reverted module call left the manual nonce counter drifted from the module account's EVM nonce")

	counter, evmNonce = moduleNonceState(t, chainApp, ctx)
	require.Equal(t, counter, evmNonce,
		"module nonce must be back in sync after the follow-up deposit")
}

// TestModuleSenderNonceDistinctHashesAcrossFailedAttempt is the residual that
// decides the design (recommendation 4 in the write-up).
//
// Making the module account's EVM nonce the source of truth *without* advancing
// it removes the drift, but re-introduces the bug the counter was added for: the
// derived tx hash is a pure function of {Nonce, GasFeeCap, GasTipCap, Gas, To,
// Value, Data}, so byte-identical module calls collide. A failed attempt is the
// sharpest case — it commits nothing at all — but on this EVM fork plain
// successes collide too, because x/vm never advances a CALL sender's nonce.
//
// Note on the failed attempt: a module-sender call passes gasLimit == nil, so
// DerivedEVMCallWithData runs EstimateGasInternal first. For an always-reverting
// call that returns EstimateGasResponse{Gas: 0, VmError: "execution reverted"},
// and the call then dies in ApplyMessageWithConfig with "intrinsic gas too low"
// before any ethereum_tx event is emitted. So the failed attempt has no hash of
// its own to compare — what it must still do is consume a nonce, so that the
// byte-identical call after it cannot reproduce the hash of the byte-identical
// call before it.
func TestModuleSenderNonceDistinctHashesAcrossFailedAttempt(t *testing.T) {
	chainApp, ctx, _ := utils.SetAppWithValidators(t)

	prc20 := utils.GetDefaultAddresses().PRC20USDCAddr
	codelessPRC20 := common.HexToAddress("0x000000000000000000000000000000000000dEaD")
	recipient := common.HexToAddress("0x0000000000000000000000000000000000001234")
	amount := big.NewInt(1_000_000)

	// Three byte-identical calls — same contract, same value, same calldata, same
	// gas limit — with a failed attempt wedged between the first and the second.
	first, err := callDepositPRC20(t, chainApp, ctx, prc20, recipient, amount)
	require.NoError(t, err)
	require.Len(t, first, 1, "a committed module deposit must emit exactly one ethereum_tx")

	failed, err := callDepositPRC20(t, chainApp, ctx, codelessPRC20, recipient, amount)
	require.Error(t, err, "depositing a codeless PRC20 must fail")
	require.Empty(t, failed, "a module call that dies in gas estimation emits no ethereum_tx")

	second, err := callDepositPRC20(t, chainApp, ctx, prc20, recipient, amount)
	require.NoError(t, err)
	require.Len(t, second, 1)

	third, err := callDepositPRC20(t, chainApp, ctx, prc20, recipient, amount)
	require.NoError(t, err)
	require.Len(t, third, 1)

	require.NotEqual(t, first[0], second[0],
		"byte-identical module calls separated by a failed attempt produced the same derived tx hash")
	require.NotEqual(t, second[0], third[0],
		"consecutive byte-identical module calls produced the same derived tx hash")
	require.NotEqual(t, first[0], third[0],
		"byte-identical module calls produced the same derived tx hash")
}
