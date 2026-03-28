package keeper_test

import (
	"fmt"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/pushchain/push-chain-node/x/uexecutor/types"
	"github.com/stretchr/testify/require"
)

func setupPendingOutboundFixture(t *testing.T) *testFixture {
	t.Helper()
	f := SetupTest(t)

	// Setup EVM mock for InitGenesis
	f.mockEVMKeeper.EXPECT().SetAccount(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	f.mockEVMKeeper.EXPECT().SetCode(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	f.mockEVMKeeper.EXPECT().SetState(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	f.k.InitGenesis(f.ctx, &types.GenesisState{Params: types.DefaultParams()})
	return f
}

func TestPendingOutbound_IndexOnCreate(t *testing.T) {
	f := setupPendingOutboundFixture(t)
	require := require.New(t)

	// Manually set a PendingOutbound entry (simulating what attachOutboundsToUtx does)
	entry := types.PendingOutboundEntry{
		OutboundId:    "outbound-1",
		UniversalTxId: "utx-1",
		CreatedAt:     100,
	}
	err := f.k.PendingOutbounds.Set(f.ctx, "outbound-1", entry)
	require.NoError(err)

	// Verify it exists
	got, err := f.k.PendingOutbounds.Get(f.ctx, "outbound-1")
	require.NoError(err)
	require.Equal("outbound-1", got.OutboundId)
	require.Equal("utx-1", got.UniversalTxId)
	require.Equal(int64(100), got.CreatedAt)
}

func TestPendingOutbound_RemoveOnVote(t *testing.T) {
	f := setupPendingOutboundFixture(t)
	require := require.New(t)

	// Set entry
	err := f.k.PendingOutbounds.Set(f.ctx, "outbound-1", types.PendingOutboundEntry{
		OutboundId:    "outbound-1",
		UniversalTxId: "utx-1",
		CreatedAt:     100,
	})
	require.NoError(err)

	// Verify exists
	has, err := f.k.PendingOutbounds.Has(f.ctx, "outbound-1")
	require.NoError(err)
	require.True(has)

	// Remove (simulating what VoteOutbound does)
	err = f.k.PendingOutbounds.Remove(f.ctx, "outbound-1")
	require.NoError(err)

	// Verify removed
	has, err = f.k.PendingOutbounds.Has(f.ctx, "outbound-1")
	require.NoError(err)
	require.False(has)
}

func TestPendingOutbound_GetPendingOutbound(t *testing.T) {
	f := setupPendingOutboundFixture(t)
	require := require.New(t)

	// Create a UTX with an outbound
	utx := types.UniversalTx{
		Id: "utx-1",
		OutboundTx: []*types.OutboundTx{
			{
				Id:               "outbound-1",
				DestinationChain: "eip155:1",
				Recipient:        "0xrecipient",
				Amount:           "1000",
				Sender:           "0xsender",
				OutboundStatus:   types.Status_PENDING,
			},
		},
	}
	require.NoError(f.k.UniversalTx.Set(f.ctx, "utx-1", utx))

	// Index the pending outbound
	require.NoError(f.k.PendingOutbounds.Set(f.ctx, "outbound-1", types.PendingOutboundEntry{
		OutboundId:    "outbound-1",
		UniversalTxId: "utx-1",
		CreatedAt:     50,
	}))

	// Query via querier
	resp, err := f.queryServer.GetPendingOutbound(f.ctx, &types.QueryGetPendingOutboundRequest{
		OutboundId: "outbound-1",
	})
	require.NoError(err)
	require.NotNil(resp.Entry)
	require.NotNil(resp.Outbound)
	require.Equal("outbound-1", resp.Entry.OutboundId)
	require.Equal("utx-1", resp.Entry.UniversalTxId)
	require.Equal("eip155:1", resp.Outbound.DestinationChain)
	require.Equal("0xrecipient", resp.Outbound.Recipient)
	require.Equal("1000", resp.Outbound.Amount)
}

func TestPendingOutbound_GetPendingOutbound_NotFound(t *testing.T) {
	f := setupPendingOutboundFixture(t)
	require := require.New(t)

	_, err := f.queryServer.GetPendingOutbound(f.ctx, &types.QueryGetPendingOutboundRequest{
		OutboundId: "nonexistent",
	})
	require.Error(err)
	require.Contains(err.Error(), "not found")
}

func TestPendingOutbound_GetPendingOutbound_EmptyId(t *testing.T) {
	f := setupPendingOutboundFixture(t)
	require := require.New(t)

	_, err := f.queryServer.GetPendingOutbound(f.ctx, &types.QueryGetPendingOutboundRequest{
		OutboundId: "",
	})
	require.Error(err)
	require.Contains(err.Error(), "outbound_id is required")
}

func TestPendingOutbound_AllPendingOutbounds(t *testing.T) {
	f := setupPendingOutboundFixture(t)
	require := require.New(t)

	// Create 3 UTXs with outbounds
	for i := 0; i < 3; i++ {
		utxId := fmt.Sprintf("utx-%d", i)
		outboundId := fmt.Sprintf("outbound-%d", i)

		utx := types.UniversalTx{
			Id: utxId,
			OutboundTx: []*types.OutboundTx{
				{
					Id:               outboundId,
					DestinationChain: fmt.Sprintf("eip155:%d", i+1),
					Recipient:        fmt.Sprintf("0xrecipient%d", i),
					Amount:           fmt.Sprintf("%d000", i+1),
					Sender:           "0xsender",
					OutboundStatus:   types.Status_PENDING,
				},
			},
		}
		require.NoError(f.k.UniversalTx.Set(f.ctx, utxId, utx))
		require.NoError(f.k.PendingOutbounds.Set(f.ctx, outboundId, types.PendingOutboundEntry{
			OutboundId:    outboundId,
			UniversalTxId: utxId,
			CreatedAt:     int64(i + 1),
		}))
	}

	// Query all
	resp, err := f.queryServer.AllPendingOutbounds(f.ctx, &types.QueryAllPendingOutboundsRequest{})
	require.NoError(err)
	require.Len(resp.Entries, 3)
	require.Len(resp.Outbounds, 3)

	// Verify outbounds have full data
	for _, ob := range resp.Outbounds {
		require.NotEmpty(ob.DestinationChain)
		require.NotEmpty(ob.Recipient)
		require.NotEmpty(ob.Amount)
	}
}

func TestPendingOutbound_AllPendingOutbounds_Empty(t *testing.T) {
	f := setupPendingOutboundFixture(t)
	require := require.New(t)

	resp, err := f.queryServer.AllPendingOutbounds(f.ctx, &types.QueryAllPendingOutboundsRequest{})
	require.NoError(err)
	require.Empty(resp.Entries)
}

func TestPendingOutbound_MultipleOutboundsPerUTX(t *testing.T) {
	f := setupPendingOutboundFixture(t)
	require := require.New(t)

	// Create a UTX with 2 outbounds
	utx := types.UniversalTx{
		Id: "utx-multi",
		OutboundTx: []*types.OutboundTx{
			{
				Id:               "outbound-a",
				DestinationChain: "eip155:1",
				Recipient:        "0xrecipientA",
				Amount:           "1000",
				OutboundStatus:   types.Status_PENDING,
			},
			{
				Id:               "outbound-b",
				DestinationChain: "eip155:137",
				Recipient:        "0xrecipientB",
				Amount:           "2000",
				OutboundStatus:   types.Status_PENDING,
			},
		},
	}
	require.NoError(f.k.UniversalTx.Set(f.ctx, "utx-multi", utx))

	// Index both
	require.NoError(f.k.PendingOutbounds.Set(f.ctx, "outbound-a", types.PendingOutboundEntry{
		OutboundId: "outbound-a", UniversalTxId: "utx-multi", CreatedAt: 1,
	}))
	require.NoError(f.k.PendingOutbounds.Set(f.ctx, "outbound-b", types.PendingOutboundEntry{
		OutboundId: "outbound-b", UniversalTxId: "utx-multi", CreatedAt: 2,
	}))

	// Query individual
	respA, err := f.queryServer.GetPendingOutbound(f.ctx, &types.QueryGetPendingOutboundRequest{OutboundId: "outbound-a"})
	require.NoError(err)
	require.Equal("eip155:1", respA.Outbound.DestinationChain)

	respB, err := f.queryServer.GetPendingOutbound(f.ctx, &types.QueryGetPendingOutboundRequest{OutboundId: "outbound-b"})
	require.NoError(err)
	require.Equal("eip155:137", respB.Outbound.DestinationChain)

	// Query all — should return both
	resp, err := f.queryServer.AllPendingOutbounds(f.ctx, &types.QueryAllPendingOutboundsRequest{})
	require.NoError(err)
	require.Len(resp.Entries, 2)
	require.Len(resp.Outbounds, 2)
}
