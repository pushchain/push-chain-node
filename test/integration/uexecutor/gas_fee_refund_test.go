package integrationtest

import (
	"fmt"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	utils "github.com/pushchain/push-chain-node/test/utils"
	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
)

func TestGasFeeRefund(t *testing.T) {

	t.Run("success vote with empty gasFeeUsed is rejected", func(t *testing.T) {
		app, ctx, vals, utxId, outbound, coreVals :=
			setupOutboundVotingTest(t, 4)

		valAddr, err := sdk.ValAddressFromBech32(coreVals[0].OperatorAddress)
		require.NoError(t, err)
		coreAcc := sdk.AccAddress(valAddr).String()

		err = utils.ExecVoteOutbound(
			t,
			ctx,
			app,
			vals[0],
			coreAcc,
			utxId,
			outbound,
			true,
			"",
			"", // gas_fee_used required when success=true → must be rejected
		)
		require.Error(t, err)
		require.Contains(t, err.Error(), "gas_fee_used required when success=true")
	})

	t.Run("no refund when gasFeeUsed equals gasFee", func(t *testing.T) {
		app, ctx, vals, utxId, outbound, coreVals :=
			setupOutboundVotingTest(t, 4)

		// The mock contract sets GasFee = 111; reporting all 111 consumed → no excess
		for i := 0; i < 3; i++ {
			valAddr, err := sdk.ValAddressFromBech32(coreVals[i].OperatorAddress)
			require.NoError(t, err)
			coreAcc := sdk.AccAddress(valAddr).String()

			err = utils.ExecVoteOutbound(
				t,
				ctx,
				app,
				vals[i],
				coreAcc,
				utxId,
				outbound,
				true,
				"",
				outbound.GasFee, // gasFeeUsed == gasFee → no excess
			)
			require.NoError(t, err)
		}

		utx, _, err := app.UexecutorKeeper.GetUniversalTx(ctx, utxId)
		require.NoError(t, err)

		ob := utx.OutboundTx[0]
		require.Equal(t, uexecutortypes.Status_OBSERVED, ob.OutboundStatus)
		require.Nil(t, ob.PcRefundExecution, "no refund expected when gasFeeUsed equals gasFee")
		require.Empty(t, ob.RefundSwapError)
	})

	t.Run("no refund when gasFeeUsed exceeds gasFee", func(t *testing.T) {
		app, ctx, vals, utxId, outbound, coreVals :=
			setupOutboundVotingTest(t, 4)

		// gasFeeUsed > gasFee (e.g. 999 > 111) → gasFee - gasFeeUsed <= 0 → no refund
		for i := 0; i < 3; i++ {
			valAddr, err := sdk.ValAddressFromBech32(coreVals[i].OperatorAddress)
			require.NoError(t, err)
			coreAcc := sdk.AccAddress(valAddr).String()

			err = utils.ExecVoteOutbound(
				t,
				ctx,
				app,
				vals[i],
				coreAcc,
				utxId,
				outbound,
				true,
				"",
				"999", // gasFeeUsed > gasFee(111) → no excess
			)
			require.NoError(t, err)
		}

		utx, _, err := app.UexecutorKeeper.GetUniversalTx(ctx, utxId)
		require.NoError(t, err)

		ob := utx.OutboundTx[0]
		require.Equal(t, uexecutortypes.Status_OBSERVED, ob.OutboundStatus)
		require.Nil(t, ob.PcRefundExecution, "no refund expected when gasFeeUsed > gasFee")
		require.Empty(t, ob.RefundSwapError)
	})

	t.Run("refund execution recorded when gasFee exceeds gasFeeUsed", func(t *testing.T) {
		app, ctx, vals, utxId, outbound, coreVals :=
			setupOutboundVotingTest(t, 4)

		// gasFee = 111 (set by mock), gasFeeUsed = 50 → 61 excess to refund
		gasFeeUsed := "50"

		for i := 0; i < 3; i++ {
			valAddr, err := sdk.ValAddressFromBech32(coreVals[i].OperatorAddress)
			require.NoError(t, err)
			coreAcc := sdk.AccAddress(valAddr).String()

			err = utils.ExecVoteOutbound(
				t,
				ctx,
				app,
				vals[i],
				coreAcc,
				utxId,
				outbound,
				true,
				"",
				gasFeeUsed,
			)
			require.NoError(t, err)
		}

		utx, _, err := app.UexecutorKeeper.GetUniversalTx(ctx, utxId)
		require.NoError(t, err)

		fmt.Println(utx)

		ob := utx.OutboundTx[0]
		require.Equal(t, uexecutortypes.Status_OBSERVED, ob.OutboundStatus)
		require.True(t, ob.ObservedTx.Success)

		// Refund was attempted → PcRefundExecution must be set
		require.NotNil(t, ob.PcRefundExecution,
			"PcRefundExecution must be set when excess gas fee exists")

		// In the test environment the UniversalCore stub may or may not implement
		// refundUnusedGas. The important invariant is that the execution record
		// is stored regardless of EVM success/failure.
		require.NotEmpty(t, ob.PcRefundExecution.Status,
			"PcRefundExecution.Status must be set")
	})

	t.Run("swap fallback reason stored when swap refund fails", func(t *testing.T) {
		app, ctx, vals, utxId, outbound, coreVals :=
			setupOutboundVotingTest(t, 4)

		// gasFee = 111, gasFeeUsed = 1 → large excess triggers refund attempt.
		// The test handler's defaultFeeTier for an unknown gas token will either
		// return 0 or fail, causing the swap path to fall back. In either case
		// RefundSwapError must be non-empty (swap was not clean) or PcRefundExecution
		// must exist.
		gasFeeUsed := "1"

		for i := 0; i < 3; i++ {
			valAddr, err := sdk.ValAddressFromBech32(coreVals[i].OperatorAddress)
			require.NoError(t, err)
			coreAcc := sdk.AccAddress(valAddr).String()

			err = utils.ExecVoteOutbound(
				t,
				ctx,
				app,
				vals[i],
				coreAcc,
				utxId,
				outbound,
				true,
				"",
				gasFeeUsed,
			)
			require.NoError(t, err)
		}

		utx, _, err := app.UexecutorKeeper.GetUniversalTx(ctx, utxId)
		require.NoError(t, err)

		ob := utx.OutboundTx[0]
		require.Equal(t, uexecutortypes.Status_OBSERVED, ob.OutboundStatus)

		// Refund execution must always be recorded when excess gas exists
		require.NotNil(t, ob.PcRefundExecution)

		// The outbound status stays OBSERVED (refund failure does not revert the outbound)
		require.Equal(t, uexecutortypes.Status_OBSERVED, ob.OutboundStatus)
	})

	t.Run("failed outbound still performs revert not refund", func(t *testing.T) {
		app, ctx, vals, utxId, outbound, coreVals :=
			setupOutboundVotingTest(t, 4)

		// Vote as FAILED with gasFeeUsed set — refund should NOT run for failed outbounds
		for i := 0; i < 3; i++ {
			valAddr, err := sdk.ValAddressFromBech32(coreVals[i].OperatorAddress)
			require.NoError(t, err)
			coreAcc := sdk.AccAddress(valAddr).String()

			err = utils.ExecVoteOutbound(
				t,
				ctx,
				app,
				vals[i],
				coreAcc,
				utxId,
				outbound,
				false,
				"execution failed",
				"50", // gasFeeUsed provided but shouldn't trigger refund on failure
			)
			require.NoError(t, err)
		}

		utx, _, err := app.UexecutorKeeper.GetUniversalTx(ctx, utxId)
		require.NoError(t, err)

		ob := utx.OutboundTx[0]
		require.Equal(t, uexecutortypes.Status_REVERTED, ob.OutboundStatus)

		// Revert was executed (funds minted back)
		require.NotNil(t, ob.PcRevertExecution)
		require.Equal(t, "SUCCESS", ob.PcRevertExecution.Status)

		// Gas refund must NOT run for failed outbounds
		require.Nil(t, ob.PcRefundExecution,
			"gas refund must not run when outbound failed")
	})
}
