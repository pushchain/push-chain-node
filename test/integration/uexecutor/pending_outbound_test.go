package integrationtest

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	uexecutorkeeper "github.com/pushchain/push-chain-node/x/uexecutor/keeper"
	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
	utils "github.com/pushchain/push-chain-node/test/utils"
)

func TestPendingOutboundIntegration(t *testing.T) {

	t.Run("outbound is indexed in PendingOutbounds after inbound creates it", func(t *testing.T) {
		app, ctx, _, utxId, outbound, _ := setupOutboundVotingTest(t, 4)

		// Outbound should be PENDING
		require.Equal(t, uexecutortypes.Status_PENDING, outbound.OutboundStatus)

		// Should exist in PendingOutbounds index
		entry, err := app.UexecutorKeeper.PendingOutbounds.Get(ctx, outbound.Id)
		require.NoError(t, err)
		require.Equal(t, outbound.Id, entry.OutboundId)
		require.Equal(t, utxId, entry.UniversalTxId)
	})

	t.Run("GetPendingOutbound query returns entry and full outbound", func(t *testing.T) {
		app, ctx, _, _, outbound, _ := setupOutboundVotingTest(t, 4)

		querier := uexecutorkeeper.NewQuerier(app.UexecutorKeeper)
		resp, err := querier.GetPendingOutbound(ctx, &uexecutortypes.QueryGetPendingOutboundRequest{
			OutboundId: outbound.Id,
		})
		require.NoError(t, err)
		require.NotNil(t, resp.Entry)
		require.NotNil(t, resp.Outbound)
		require.Equal(t, outbound.Id, resp.Entry.OutboundId)
		require.Equal(t, outbound.DestinationChain, resp.Outbound.DestinationChain)
		require.Equal(t, outbound.Recipient, resp.Outbound.Recipient)
		require.Equal(t, outbound.Amount, resp.Outbound.Amount)
		require.Equal(t, outbound.ExternalAssetAddr, resp.Outbound.ExternalAssetAddr)
	})

	t.Run("AllPendingOutbounds returns entries with full outbound data", func(t *testing.T) {
		app, ctx, _, _, outbound, _ := setupOutboundVotingTest(t, 4)

		querier := uexecutorkeeper.NewQuerier(app.UexecutorKeeper)
		resp, err := querier.AllPendingOutbounds(ctx, &uexecutortypes.QueryAllPendingOutboundsRequest{})
		require.NoError(t, err)
		require.Len(t, resp.Entries, 1)
		require.Len(t, resp.Outbounds, 1)
		require.Equal(t, outbound.Id, resp.Entries[0].OutboundId)
		require.Equal(t, outbound.DestinationChain, resp.Outbounds[0].DestinationChain)
	})

	t.Run("outbound removed from index after successful vote quorum", func(t *testing.T) {
		app, ctx, vals, utxId, outbound, coreVals := setupOutboundVotingTest(t, 4)

		// Verify indexed
		has, err := app.UexecutorKeeper.PendingOutbounds.Has(ctx, outbound.Id)
		require.NoError(t, err)
		require.True(t, has)

		// Reach quorum with success
		for i := 0; i < 3; i++ {
			valAddr, _ := sdk.ValAddressFromBech32(coreVals[i].OperatorAddress)
			coreAcc := sdk.AccAddress(valAddr).String()
			err := utils.ExecVoteOutbound(t, ctx, app, vals[i], coreAcc,
				utxId, outbound, true, "", outbound.GasFee)
			require.NoError(t, err)
		}

		// Should be removed from index
		has, err = app.UexecutorKeeper.PendingOutbounds.Has(ctx, outbound.Id)
		require.NoError(t, err)
		require.False(t, has, "outbound should be removed from PendingOutbounds after quorum")

		// AllPendingOutbounds should be empty
		querier := uexecutorkeeper.NewQuerier(app.UexecutorKeeper)
		resp, err := querier.AllPendingOutbounds(ctx, &uexecutortypes.QueryAllPendingOutboundsRequest{})
		require.NoError(t, err)
		require.Empty(t, resp.Entries)
	})

	t.Run("outbound removed from index after failed vote quorum", func(t *testing.T) {
		app, ctx, vals, utxId, outbound, coreVals := setupOutboundVotingTest(t, 4)

		// Reach quorum with failure
		for i := 0; i < 3; i++ {
			valAddr, _ := sdk.ValAddressFromBech32(coreVals[i].OperatorAddress)
			coreAcc := sdk.AccAddress(valAddr).String()
			err := utils.ExecVoteOutbound(t, ctx, app, vals[i], coreAcc,
				utxId, outbound, false, "execution reverted", outbound.GasFee)
			require.NoError(t, err)
		}

		// Should be removed (now REVERTED, not PENDING)
		has, err := app.UexecutorKeeper.PendingOutbounds.Has(ctx, outbound.Id)
		require.NoError(t, err)
		require.False(t, has, "failed outbound should be removed from PendingOutbounds")
	})

	t.Run("GetPendingOutbound returns not found for unknown ID", func(t *testing.T) {
		app, ctx, _, _, _, _ := setupOutboundVotingTest(t, 4)

		querier := uexecutorkeeper.NewQuerier(app.UexecutorKeeper)
		_, err := querier.GetPendingOutbound(ctx, &uexecutortypes.QueryGetPendingOutboundRequest{
			OutboundId: "nonexistent-id",
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "not found")
	})

	t.Run("GetPendingOutbound rejects empty ID", func(t *testing.T) {
		app, ctx, _, _, _, _ := setupOutboundVotingTest(t, 4)

		querier := uexecutorkeeper.NewQuerier(app.UexecutorKeeper)
		_, err := querier.GetPendingOutbound(ctx, &uexecutortypes.QueryGetPendingOutboundRequest{
			OutboundId: "",
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "outbound_id is required")
	})
}
