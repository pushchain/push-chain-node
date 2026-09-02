package integrationtest

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	utils "github.com/pushchain/push-chain-node/test/utils"
	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
)

// TestInboundRevertedPayloadBillsGas is the regression test for F-2026-18824 rec 2.
//
// When an inbound payload reverts, its EVM state is discarded — but the gas it burned
// on the way there is real work the validators performed. It used to be free:
// DerivedEVMCall returned a nil response on a VM failure, so the caller's fee deduction
// had nothing to bill against and a reverting payload paid nothing at all.
//
// The UEA here is funded with upc directly by the setup and the inbound deposit is a
// PRC20, so upc moves for exactly one reason: gas.
func TestInboundRevertedPayloadBillsGas(t *testing.T) {
	prc20 := utils.GetDefaultAddresses().PRC20USDCAddr

	chainApp, ctx, vals, coreVals, ueaAddr := setupMulticallOutboundTest(t, 4)
	ueaAcc := sdk.AccAddress(ueaAddr.Bytes())
	upcBefore := chainApp.BankKeeper.GetBalance(ctx, ueaAcc, "upc")

	// 0xdeadbeef matches no selector on the PRC20, which has no fallback, so the
	// call reverts — after the UEA has already paid to dispatch it.
	inbound := multicallInbound("0xreverted-payload-gas-01", uexecutortypes.TxType_FUNDS_AND_PAYLOAD, "1000000", "0xdeadbeef")
	inbound.UniversalPayload.To = prc20.Hex()

	voteToQuorum(t, ctx, chainApp, vals, coreVals, inbound)

	utxKey := uexecutortypes.GetInboundUniversalTxKey(*inbound)
	utx, found, err := chainApp.UexecutorKeeper.GetUniversalTx(ctx, utxKey)
	require.NoError(t, err)
	require.True(t, found, "the inbound must be recorded even though its payload reverted")

	// Guard the premise. Without this a payload that silently stopped reverting —
	// or one that never ran at all — would make the balance assertion meaningless.
	pcTx := payloadPcTx(t, utx)
	require.Equal(t, "FAILED", pcTx.Status, "the payload must have reverted for this test to mean anything")
	require.NotContains(t, pcTx.ErrorMsg, "depositAutoSwap failed",
		"the payload must be what failed, not the funding step before it")

	upcAfter := chainApp.BankKeeper.GetBalance(ctx, ueaAcc, "upc")

	// The fix: the reverted payload's gas came out of the UEA.
	require.True(t, upcAfter.Amount.LT(upcBefore.Amount),
		"a reverted payload must still be billed for the gas it burned (before=%s, after=%s)",
		upcBefore.Amount, upcAfter.Amount)

	// Billing is best-effort and clamped to the available balance, so it can never
	// overdraw the UEA or fail the inbound.
	require.False(t, upcAfter.Amount.IsNegative(), "billing must never drive the UEA balance negative")
}
