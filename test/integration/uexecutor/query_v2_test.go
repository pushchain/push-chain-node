package integrationtest

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	utils "github.com/pushchain/push-chain-node/test/utils"
	uexecutorkeeper "github.com/pushchain/push-chain-node/x/uexecutor/keeper"
	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
	typesv2 "github.com/pushchain/push-chain-node/x/uexecutor/typesv2"
)

func TestGetUniversalTxV2(t *testing.T) {
	t.Run("returns NotFound for unknown ID", func(t *testing.T) {
		chainApp, ctx, _, _, _, _ := setupInboundCEAPayloadTest(t, 4)

		q := uexecutorkeeper.QuerierV2{Keeper: chainApp.UexecutorKeeper}
		_, err := q.GetUniversalTx(sdk.WrapSDKContext(ctx), &typesv2.QueryGetUniversalTxRequest{
			Id: "nonexistent-tx-id",
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.NotFound, st.Code())
	})

	t.Run("nil request returns InvalidArgument", func(t *testing.T) {
		chainApp, ctx, _, _, _, _ := setupInboundCEAPayloadTest(t, 4)

		q := uexecutorkeeper.QuerierV2{Keeper: chainApp.UexecutorKeeper}
		_, err := q.GetUniversalTx(sdk.WrapSDKContext(ctx), nil)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("returns native UniversalTx after FUNDS inbound execution", func(t *testing.T) {
		chainApp, ctx, vals, inbound, coreVals := setupInboundBridgeTest(t, 4)

		// Reach quorum
		for i := 0; i < 3; i++ {
			valAddr, err := sdk.ValAddressFromBech32(coreVals[i].OperatorAddress)
			require.NoError(t, err)
			coreValAcc := sdk.AccAddress(valAddr).String()
			err = utils.ExecVoteInbound(t, ctx, chainApp, vals[i], coreValAcc, inbound)
			require.NoError(t, err)
		}

		q := uexecutorkeeper.QuerierV2{Keeper: chainApp.UexecutorKeeper}
		resp, err := q.GetUniversalTx(sdk.WrapSDKContext(ctx), &typesv2.QueryGetUniversalTxRequest{
			Id: uexecutortypes.GetInboundUniversalTxKey(*inbound),
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.NotNil(t, resp.UniversalTx)

		utx := resp.UniversalTx

		// InboundTx fields are fully preserved
		require.NotNil(t, utx.InboundTx)
		require.Equal(t, inbound.SourceChain, utx.InboundTx.SourceChain)
		require.Equal(t, inbound.TxHash, utx.InboundTx.TxHash)
		require.Equal(t, inbound.Sender, utx.InboundTx.Sender)
		require.Equal(t, inbound.Amount, utx.InboundTx.Amount)
		require.Equal(t, inbound.AssetAddr, utx.InboundTx.AssetAddr)

		// Native TxType enum — not mapped to legacy enum values
		require.Equal(t, uexecutortypes.TxType_FUNDS, utx.InboundTx.TxType)

		// RevertInstructions is preserved (dropped by legacy conversion)
		require.NotNil(t, utx.InboundTx.RevertInstructions)
		require.Equal(t, inbound.RevertInstructions.FundRecipient, utx.InboundTx.RevertInstructions.FundRecipient)
	})

	t.Run("returns native UniversalTx after FUNDS_AND_PAYLOAD inbound execution", func(t *testing.T) {
		chainApp, ctx, vals, inbound, coreVals, _ := setupInboundBridgePayloadTest(t, 4)

		for i := 0; i < 3; i++ {
			valAddr, err := sdk.ValAddressFromBech32(coreVals[i].OperatorAddress)
			require.NoError(t, err)
			coreValAcc := sdk.AccAddress(valAddr).String()
			err = utils.ExecVoteInbound(t, ctx, chainApp, vals[i], coreValAcc, inbound)
			require.NoError(t, err)
		}

		q := uexecutorkeeper.QuerierV2{Keeper: chainApp.UexecutorKeeper}
		resp, err := q.GetUniversalTx(sdk.WrapSDKContext(ctx), &typesv2.QueryGetUniversalTxRequest{
			Id: uexecutortypes.GetInboundUniversalTxKey(*inbound),
		})
		require.NoError(t, err)
		require.NotNil(t, resp.UniversalTx)

		// Native TxType_FUNDS_AND_PAYLOAD (4), not the legacy value (3)
		require.Equal(t, uexecutortypes.TxType_FUNDS_AND_PAYLOAD, resp.UniversalTx.InboundTx.TxType)

		// PcTx entries present
		require.NotEmpty(t, resp.UniversalTx.PcTx)
	})

	t.Run("IsCEA=true is preserved in v2 response", func(t *testing.T) {
		chainApp, ctx, vals, inbound, coreVals, _ := setupInboundCEAPayloadTest(t, 4)

		for i := 0; i < 3; i++ {
			valAddr, err := sdk.ValAddressFromBech32(coreVals[i].OperatorAddress)
			require.NoError(t, err)
			coreValAcc := sdk.AccAddress(valAddr).String()
			err = utils.ExecVoteInbound(t, ctx, chainApp, vals[i], coreValAcc, inbound)
			require.NoError(t, err)
		}

		q := uexecutorkeeper.QuerierV2{Keeper: chainApp.UexecutorKeeper}
		resp, err := q.GetUniversalTx(sdk.WrapSDKContext(ctx), &typesv2.QueryGetUniversalTxRequest{
			Id: uexecutortypes.GetInboundUniversalTxKey(*inbound),
		})
		require.NoError(t, err)
		require.NotNil(t, resp.UniversalTx)

		// isCEA flag is preserved — this is a new field not present in legacy type at all
		require.True(t, resp.UniversalTx.InboundTx.IsCEA,
			"isCEA should be true and preserved in v2 response")

		// Recipient (the UEA address) is preserved
		require.Equal(t, inbound.Recipient, resp.UniversalTx.InboundTx.Recipient)
	})

	t.Run("v2 returns all outbound txs while v1 returns only first", func(t *testing.T) {
		// Set up a FUNDS inbound that will fail (no token config) to generate a revert outbound,
		// then verify v2 returns full OutboundTx slice while v1 legacy only surfaces first entry.
		chainApp, ctx, vals, inbound, coreVals := setupInboundBridgeTest(t, 4)

		// Remove token config so deposit fails and a revert outbound is created
		chainApp.UregistryKeeper.RemoveTokenConfig(ctx, inbound.SourceChain, inbound.AssetAddr)

		for i := 0; i < 3; i++ {
			valAddr, err := sdk.ValAddressFromBech32(coreVals[i].OperatorAddress)
			require.NoError(t, err)
			coreValAcc := sdk.AccAddress(valAddr).String()
			err = utils.ExecVoteInbound(t, ctx, chainApp, vals[i], coreValAcc, inbound)
			require.NoError(t, err)
		}

		q := uexecutorkeeper.QuerierV2{Keeper: chainApp.UexecutorKeeper}

		// v2 response
		respV2, err := q.GetUniversalTx(sdk.WrapSDKContext(ctx), &typesv2.QueryGetUniversalTxRequest{
			Id: uexecutortypes.GetInboundUniversalTxKey(*inbound),
		})
		require.NoError(t, err)
		require.NotNil(t, respV2.UniversalTx)

		// v2 exposes the full repeated OutboundTx slice
		require.IsType(t, []*uexecutortypes.OutboundTx{}, respV2.UniversalTx.OutboundTx,
			"v2 OutboundTx should be the repeated native type")

		// v1 response for comparison
		qV1 := uexecutorkeeper.Querier{Keeper: chainApp.UexecutorKeeper}
		respV1, err := qV1.GetUniversalTx(sdk.WrapSDKContext(ctx), &uexecutortypes.QueryGetUniversalTxRequest{
			Id: uexecutortypes.GetInboundUniversalTxKey(*inbound),
		})
		require.NoError(t, err)
		require.NotNil(t, respV1.UniversalTx)

		// v1 legacy type holds a single OutboundTxLegacy (not a slice)
		require.IsType(t, (*uexecutortypes.OutboundTxLegacy)(nil), respV1.UniversalTx.OutboundTx,
			"v1 OutboundTx should be the legacy single-entry type")
	})

	t.Run("v2 does not expose universal_status, v1 computes and returns it", func(t *testing.T) {
		chainApp, ctx, vals, inbound, coreVals := setupInboundBridgeTest(t, 4)

		for i := 0; i < 3; i++ {
			valAddr, err := sdk.ValAddressFromBech32(coreVals[i].OperatorAddress)
			require.NoError(t, err)
			coreValAcc := sdk.AccAddress(valAddr).String()
			err = utils.ExecVoteInbound(t, ctx, chainApp, vals[i], coreValAcc, inbound)
			require.NoError(t, err)
		}

		utxKey := uexecutortypes.GetInboundUniversalTxKey(*inbound)

		// v2: UniversalTx has no universal_status field — confirmed at compile time by proto removal
		qV2 := uexecutorkeeper.QuerierV2{Keeper: chainApp.UexecutorKeeper}
		respV2, err := qV2.GetUniversalTx(sdk.WrapSDKContext(ctx), &typesv2.QueryGetUniversalTxRequest{
			Id: utxKey,
		})
		require.NoError(t, err)
		require.NotNil(t, respV2.UniversalTx)
		require.NotEmpty(t, respV2.UniversalTx.PcTx, "v2 should have pc_tx after execution")

		// v1: UniversalTxLegacy still carries universal_status computed on-the-fly
		qV1 := uexecutorkeeper.Querier{Keeper: chainApp.UexecutorKeeper}
		respV1, err := qV1.GetUniversalTx(sdk.WrapSDKContext(ctx), &uexecutortypes.QueryGetUniversalTxRequest{
			Id: utxKey,
		})
		require.NoError(t, err)
		require.NotNil(t, respV1.UniversalTx)
		require.Equal(t,
			uexecutortypes.UniversalTxStatus_PC_EXECUTED_SUCCESS,
			respV1.UniversalTx.UniversalStatus,
			"v1 should return computed universal_status from pc_tx state",
		)
	})

	t.Run("v2 TxType preserves native enum, v1 maps to legacy enum", func(t *testing.T) {
		// FUNDS_AND_PAYLOAD: native TxType = 4, legacy = INBOUND_LEGACY_FUNDS_AND_PAYLOAD = 3
		chainApp, ctx, vals, inbound, coreVals, _ := setupInboundBridgePayloadTest(t, 4)

		for i := 0; i < 3; i++ {
			valAddr, err := sdk.ValAddressFromBech32(coreVals[i].OperatorAddress)
			require.NoError(t, err)
			coreValAcc := sdk.AccAddress(valAddr).String()
			err = utils.ExecVoteInbound(t, ctx, chainApp, vals[i], coreValAcc, inbound)
			require.NoError(t, err)
		}

		qV1 := uexecutorkeeper.Querier{Keeper: chainApp.UexecutorKeeper}
		qV2 := uexecutorkeeper.QuerierV2{Keeper: chainApp.UexecutorKeeper}
		id := uexecutortypes.GetInboundUniversalTxKey(*inbound)

		respV1, err := qV1.GetUniversalTx(sdk.WrapSDKContext(ctx), &uexecutortypes.QueryGetUniversalTxRequest{Id: id})
		require.NoError(t, err)

		respV2, err := qV2.GetUniversalTx(sdk.WrapSDKContext(ctx), &typesv2.QueryGetUniversalTxRequest{Id: id})
		require.NoError(t, err)

		// v1 uses legacy enum — INBOUND_LEGACY_FUNDS_AND_PAYLOAD = 3
		require.Equal(t,
			uexecutortypes.InboundTxTypeLegacy_INBOUND_LEGACY_FUNDS_AND_PAYLOAD,
			respV1.UniversalTx.InboundTx.TxType,
			"v1 should map to legacy TxType enum")

		// v2 uses native enum — TxType_FUNDS_AND_PAYLOAD = 4
		require.Equal(t,
			uexecutortypes.TxType_FUNDS_AND_PAYLOAD,
			respV2.UniversalTx.InboundTx.TxType,
			"v2 should return native TxType enum")
	})

	t.Run("v2 pc_tx hashes match direct keeper fetch", func(t *testing.T) {
		chainApp, ctx, vals, inbound, coreVals := setupInboundBridgeTest(t, 4)

		for i := 0; i < 3; i++ {
			valAddr, err := sdk.ValAddressFromBech32(coreVals[i].OperatorAddress)
			require.NoError(t, err)
			coreValAcc := sdk.AccAddress(valAddr).String()
			err = utils.ExecVoteInbound(t, ctx, chainApp, vals[i], coreValAcc, inbound)
			require.NoError(t, err)
		}

		utxKey := uexecutortypes.GetInboundUniversalTxKey(*inbound)
		directUtx, found, err := chainApp.UexecutorKeeper.GetUniversalTx(ctx, utxKey)
		require.NoError(t, err)
		require.True(t, found)

		q := uexecutorkeeper.QuerierV2{Keeper: chainApp.UexecutorKeeper}
		resp, err := q.GetUniversalTx(sdk.WrapSDKContext(ctx), &typesv2.QueryGetUniversalTxRequest{
			Id: utxKey,
		})
		require.NoError(t, err)

		require.Len(t, resp.UniversalTx.PcTx, len(directUtx.PcTx))
		for i, pcTx := range resp.UniversalTx.PcTx {
			require.Equal(t, directUtx.PcTx[i].TxHash, pcTx.TxHash)
			require.Equal(t, directUtx.PcTx[i].Status, pcTx.Status)
		}
	})
}
