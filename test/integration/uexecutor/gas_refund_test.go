package integrationtest

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	utils "github.com/pushchain/push-chain-node/test/utils"
	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
)

// TestInboundRevertGasNotRefunded proves an INBOUND_REVERT never refunds gas on
// settlement. A revert is protocol-initiated — the user was never charged a gas fee
// for it — so even when a GasFee budget is present (PRC20 reverts set one) and the
// observed gasFeeUsed is well below it, no refund must be attempted.
func TestInboundRevertGasNotRefunded(t *testing.T) {
	chainApp, ctx, vals, utxId, ob, coreVals := setupOutboundVotingTest(t, 4)

	// Make the seeded outbound an INBOUND_REVERT carrying a gas budget with headroom
	// that would otherwise trigger a refund (GasFee 1000, gasFeeUsed 100 below).
	ob.TxType = uexecutortypes.TxType_INBOUND_REVERT
	ob.GasFee = "1000"
	ob.GasToken = "0x000000000000000000000000000000000000C0dE"
	require.NoError(t, chainApp.UexecutorKeeper.UpdateOutbound(ctx, utxId, *ob))

	// Settle it successfully with gasFeeUsed << GasFee.
	for i := 0; i < 3; i++ {
		valAddr, err := sdk.ValAddressFromBech32(coreVals[i].OperatorAddress)
		require.NoError(t, err)
		require.NoError(t, utils.ExecVoteOutbound(
			t, ctx, chainApp, vals[i], sdk.AccAddress(valAddr).String(), utxId, ob, true, "", "100"))
	}

	utx, found, err := chainApp.UexecutorKeeper.GetUniversalTx(ctx, utxId)
	require.NoError(t, err)
	require.True(t, found)
	require.Nil(t, utx.OutboundTx[0].PcRefundExecution,
		"INBOUND_REVERT must not attempt a gas refund — the user was never charged for it")
}
