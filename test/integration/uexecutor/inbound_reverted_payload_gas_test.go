package integrationtest

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	utils "github.com/pushchain/push-chain-node/test/utils"
	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
)

// TestInboundRevertedPayloadBillsGas covers F-2026-18824 rec 2: a reverted inbound
// payload used to pay no gas at all. The UEA is funded with upc by the setup and the
// deposit is a PRC20, so upc moves for exactly one reason here — gas.
func TestInboundRevertedPayloadBillsGas(t *testing.T) {
	prc20 := utils.GetDefaultAddresses().PRC20USDCAddr

	chainApp, ctx, vals, coreVals, ueaAddr := setupMulticallOutboundTest(t, 4)
	ueaAcc := sdk.AccAddress(ueaAddr.Bytes())
	upcBefore := chainApp.BankKeeper.GetBalance(ctx, ueaAcc, "upc")

	// 0xdeadbeef matches no selector on the PRC20, which has no fallback -> revert.
	inbound := multicallInbound("0xreverted-payload-gas-01", uexecutortypes.TxType_FUNDS_AND_PAYLOAD, "1000000", "0xdeadbeef")
	inbound.UniversalPayload.To = prc20.Hex()

	voteToQuorum(t, ctx, chainApp, vals, coreVals, inbound)

	utxKey := uexecutortypes.GetInboundUniversalTxKey(*inbound)
	utx, found, err := chainApp.UexecutorKeeper.GetUniversalTx(ctx, utxKey)
	require.NoError(t, err)
	require.True(t, found, "the inbound must be recorded even though its payload reverted")

	// Guard the premise: a payload that stopped reverting, or never ran, would make
	// the balance assertion below meaningless.
	pcTx := payloadPcTx(t, utx)
	require.Equal(t, "FAILED", pcTx.Status, "the payload must have reverted for this test to mean anything")
	require.NotContains(t, pcTx.ErrorMsg, "depositAutoSwap failed",
		"the payload must be what failed, not the funding step before it")

	upcAfter := chainApp.BankKeeper.GetBalance(ctx, ueaAcc, "upc")

	// The fix.
	require.True(t, upcAfter.Amount.LT(upcBefore.Amount),
		"a reverted payload must still be billed for the gas it burned (before=%s, after=%s)",
		upcBefore.Amount, upcAfter.Amount)

	// Billing is clamped to the available balance.
	require.False(t, upcAfter.Amount.IsNegative(), "billing must never drive the UEA balance negative")
}
