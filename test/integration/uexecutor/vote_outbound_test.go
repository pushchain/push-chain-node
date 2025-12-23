package integrationtest

import (
	"fmt"
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authz "github.com/cosmos/cosmos-sdk/x/authz"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/app"
	utils "github.com/pushchain/push-chain-node/test/utils"
	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
)

func setupOutboundVotingTest(
	t *testing.T,
	numVals int,
) (
	*app.ChainApp,
	sdk.Context,
	[]string, // universal validators
	string, // utxId
	*uexecutortypes.OutboundTx, // outbound
	[]stakingtypes.Validator, // core validators
) {

	app, ctx, universalVals, inbound, coreVals, _ :=
		setupInboundInitiatedOutboundTest(t, numVals)

	// reach quorum for inbound
	for i := 0; i < 3; i++ {
		valAddr, err := sdk.ValAddressFromBech32(coreVals[i].OperatorAddress)
		require.NoError(t, err)

		coreAcc := sdk.AccAddress(valAddr).String()
		err = utils.ExecVoteInbound(
			t,
			ctx,
			app,
			universalVals[i],
			coreAcc,
			inbound,
		)
		require.NoError(t, err)
	}

	utxId := uexecutortypes.GetInboundUniversalTxKey(*inbound)

	utx, found, err := app.UexecutorKeeper.GetUniversalTx(ctx, utxId)
	require.NoError(t, err)
	require.True(t, found)
	require.Len(t, utx.OutboundTx, 1)

	return app, ctx, universalVals, utxId, utx.OutboundTx[0], coreVals
}

func TestOutboundVoting(t *testing.T) {

	t.Run("less than quorum outbound votes keeps outbound pending", func(t *testing.T) {
		app, ctx, vals, utxId, outbound, coreVals :=
			setupOutboundVotingTest(t, 4)

		// grant authz for outbound voting
		for i, val := range coreVals {
			accAddr, err := sdk.ValAddressFromBech32(val.OperatorAddress)
			require.NoError(t, err)

			coreAcc := sdk.AccAddress(accAddr)
			uniAcc := sdk.MustAccAddressFromBech32(vals[i])

			auth := authz.NewGenericAuthorization(
				sdk.MsgTypeURL(&uexecutortypes.MsgVoteOutbound{}),
			)
			exp := ctx.BlockTime().Add(time.Hour)

			err = app.AuthzKeeper.SaveGrant(ctx, uniAcc, coreAcc, auth, &exp)
			require.NoError(t, err)
		}

		// only 1 vote
		valAddr, _ := sdk.ValAddressFromBech32(coreVals[0].OperatorAddress)
		coreAcc := sdk.AccAddress(valAddr).String()

		err := utils.ExecVoteOutbound(
			t,
			ctx,
			app,
			vals[0],
			coreAcc,
			utxId,
			outbound,
			true,
			"",
		)
		require.NoError(t, err)

		utx, _, err := app.UexecutorKeeper.GetUniversalTx(ctx, utxId)
		require.NoError(t, err)
		require.Equal(
			t,
			uexecutortypes.Status_PENDING,
			utx.OutboundTx[0].OutboundStatus,
		)
	})

	t.Run("quorum reached finalizes outbound successfully", func(t *testing.T) {
		app, ctx, vals, utxId, outbound, coreVals :=
			setupOutboundVotingTest(t, 4)

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
			)
			require.NoError(t, err)
		}

		utx, _, err := app.UexecutorKeeper.GetUniversalTx(ctx, utxId)
		require.NoError(t, err)

		ob := utx.OutboundTx[0]
		require.Equal(t, uexecutortypes.Status_OBSERVED, ob.OutboundStatus)
		require.NotNil(t, ob.ObservedTx)
		require.True(t, ob.ObservedTx.Success)
	})

	t.Run("duplicate outbound vote fails", func(t *testing.T) {
		app, ctx, vals, utxId, outbound, coreVals :=
			setupOutboundVotingTest(t, 4)

		valAddr, _ := sdk.ValAddressFromBech32(coreVals[0].OperatorAddress)
		coreAcc := sdk.AccAddress(valAddr).String()

		err := utils.ExecVoteOutbound(
			t,
			ctx,
			app,
			vals[0],
			coreAcc,
			utxId,
			outbound,
			true,
			"",
		)
		require.NoError(t, err)

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
		)
		require.Error(t, err)
		require.Contains(t, err.Error(), "already voted")
	})

	t.Run("vote after outbound finalized fails", func(t *testing.T) {
		app, ctx, vals, utxId, outbound, coreVals :=
			setupOutboundVotingTest(t, 4)

		// finalize
		for i := 0; i < 3; i++ {
			valAddr, _ := sdk.ValAddressFromBech32(coreVals[i].OperatorAddress)
			coreAcc := sdk.AccAddress(valAddr).String()

			err := utils.ExecVoteOutbound(
				t,
				ctx,
				app,
				vals[i],
				coreAcc,
				utxId,
				outbound,
				true,
				"",
			)
			require.NoError(t, err)
		}

		// extra vote
		valAddr, _ := sdk.ValAddressFromBech32(coreVals[3].OperatorAddress)
		coreAcc := sdk.AccAddress(valAddr).String()

		err := utils.ExecVoteOutbound(
			t,
			ctx,
			app,
			vals[3],
			coreAcc,
			utxId,
			outbound,
			true,
			"",
		)
		require.Error(t, err)
		require.Contains(t, err.Error(), "already finalized")
	})

	t.Run("outbound failure triggers revert execution", func(t *testing.T) {
		app, ctx, vals, utxId, outbound, coreVals :=
			setupOutboundVotingTest(t, 4)

		// Reach quorum with FAILED observation
		for i := 0; i < 3; i++ {
			valAddr, _ := sdk.ValAddressFromBech32(coreVals[i].OperatorAddress)
			coreAcc := sdk.AccAddress(valAddr).String()

			err := utils.ExecVoteOutbound(
				t,
				ctx,
				app,
				vals[i],
				coreAcc,
				utxId,
				outbound,
				false,
				"execution reverted", // revert reason
			)
			require.NoError(t, err)
		}

		utx, _, err := app.UexecutorKeeper.GetUniversalTx(ctx, utxId)
		require.NoError(t, err)

		fmt.Println(utx)

		ob := utx.OutboundTx[0]

		require.Equal(t, uexecutortypes.Status_REVERTED, ob.OutboundStatus)
		require.NotNil(t, ob.PcRevertExecution)

		pc := ob.PcRevertExecution
		require.Equal(t, "SUCCESS", pc.Status)
		require.NotEmpty(t, pc.TxHash)
	})

	t.Run("revert recipient defaults to sender when revert instructions missing", func(t *testing.T) {
		app, ctx, vals, utxId, outbound, coreVals :=
			setupOutboundVotingTest(t, 4)

		// explicitly remove revert instructions
		outbound.RevertInstructions = nil

		for i := 0; i < 3; i++ {
			valAddr, _ := sdk.ValAddressFromBech32(coreVals[i].OperatorAddress)
			coreAcc := sdk.AccAddress(valAddr).String()

			err := utils.ExecVoteOutbound(
				t,
				ctx,
				app,
				vals[i],
				coreAcc,
				utxId,
				outbound,
				false,
				"failed",
			)
			require.NoError(t, err)
		}

		utx, _, err := app.UexecutorKeeper.GetUniversalTx(ctx, utxId)
		require.NoError(t, err)

		ob := utx.OutboundTx[0]

		require.Equal(t, uexecutortypes.Status_REVERTED, ob.OutboundStatus)
		require.Equal(t, outbound.Sender, ob.PcRevertExecution.Sender)
	})

}
