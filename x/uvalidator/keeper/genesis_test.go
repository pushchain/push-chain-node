package keeper_test

import (
	"testing"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/pushchain/push-chain-node/x/uvalidator/types"
	"github.com/stretchr/testify/require"
)

func TestGenesis(t *testing.T) {
	f := SetupTest(t)

	genesisState := &types.GenesisState{
		Params: types.Params{Admin: f.addrs[0].String()},
	}

	f.k.InitGenesis(f.ctx, genesisState)

	got := f.k.ExportGenesis(f.ctx)
	require.NotNil(t, got)
}

func TestGenesisExportImportRoundTrip(t *testing.T) {
	f := SetupTest(t)
	f.k.InitGenesis(f.ctx, &types.GenesisState{Params: types.Params{Admin: f.addrs[0].String()}})

	// Populate state: add a universal validator
	valAddr := sdk.ValAddress(f.addrs[0])
	uv := types.UniversalValidator{
		IdentifyInfo: &types.IdentityInfo{
			CoreValidatorAddress: valAddr.String(),
		},
		LifecycleInfo: &types.LifecycleInfo{
			CurrentStatus: types.UVStatus_UV_STATUS_ACTIVE,
		},
	}
	require.NoError(t, f.k.UniversalValidatorSet.Set(f.ctx, valAddr, uv))

	// Populate ballots
	ballot := types.Ballot{
		Id:                "ballot-1",
		Status:            types.BallotStatus_BALLOT_STATUS_PENDING,
		BlockHeightExpiry: 100,
	}
	require.NoError(t, f.k.SetBallot(f.ctx, ballot))
	require.NoError(t, f.k.ActiveBallotIDs.Set(f.ctx, "ballot-1"))

	// Export
	exported := f.k.ExportGenesis(f.ctx)
	require.NotNil(t, exported)
	require.Len(t, exported.UniversalValidators, 1)
	require.Len(t, exported.Ballots, 1)
	require.Len(t, exported.ActiveBallotIds, 1)

	// Re-init on fresh fixture
	f2 := SetupTest(t)
	f2.k.InitGenesis(f2.ctx, exported)

	// Export again and compare
	reExported := f2.k.ExportGenesis(f2.ctx)
	require.Equal(t, len(exported.UniversalValidators), len(reExported.UniversalValidators))
	require.Equal(t, len(exported.Ballots), len(reExported.Ballots))
	require.Equal(t, len(exported.ActiveBallotIds), len(reExported.ActiveBallotIds))
	require.Equal(t, exported.UniversalValidators[0].Key, reExported.UniversalValidators[0].Key)
}

// TestInitGenesisRebuildsPendingByExpiry pins the reason this change needs no
// state migration: the expiry index is derived state that InitGenesis rebuilds
// from the ballots it just restored.
func TestInitGenesisRebuildsPendingByExpiry(t *testing.T) {
	f := SetupTest(t)
	f.k.InitGenesis(f.ctx, &types.GenesisState{Params: types.Params{Admin: f.addrs[0].String()}})

	genesis := &types.GenesisState{
		Params: types.Params{Admin: f.addrs[0].String()},
		Ballots: []types.Ballot{
			{Id: "g-1", Status: types.BallotStatus_BALLOT_STATUS_PENDING, BlockHeightExpiry: 42},
			{Id: "g-2", Status: types.BallotStatus_BALLOT_STATUS_PENDING, BlockHeightExpiry: 7},
			{Id: "g-3", Status: types.BallotStatus_BALLOT_STATUS_PASSED, BlockHeightExpiry: 9},
		},
		ActiveBallotIds:    []string{"g-1", "g-2"},
		FinalizedBallotIds: []string{"g-3"},
	}

	f2 := SetupTest(t)
	require.NoError(t, f2.k.InitGenesis(f2.ctx, genesis))

	// Both active ballots are indexed under their own expiry heights.
	for _, tc := range []struct {
		id     string
		expiry int64
	}{{"g-1", 42}, {"g-2", 7}} {
		has, err := f2.k.PendingByExpiry.Has(f2.ctx, collections.Join(tc.expiry, tc.id))
		require.NoError(t, err)
		require.True(t, has, "InitGenesis must rebuild the expiry index for %s", tc.id)
	}

	// The finalized ballot is not active, so it must not be indexed.
	has, err := f2.k.PendingByExpiry.Has(f2.ctx, collections.Join(int64(9), "g-3"))
	require.NoError(t, err)
	require.False(t, has, "only active ballots belong in the expiry index")

	// The rebuilt index drives the sweep: g-2 (expiry 7) is due at height 10,
	// g-1 (expiry 42) is not.
	require.NoError(t, f2.k.ExpireBallotsBeforeHeight(f2.ctx, 10))

	got, err := f2.k.GetBallot(f2.ctx, "g-2")
	require.NoError(t, err)
	require.Equal(t, types.BallotStatus_BALLOT_STATUS_EXPIRED, got.Status)

	got, err = f2.k.GetBallot(f2.ctx, "g-1")
	require.NoError(t, err)
	require.Equal(t, types.BallotStatus_BALLOT_STATUS_PENDING, got.Status)
}

// TestInitGenesisRejectsDanglingActiveBallotID: an active ballot id with no
// ballot record cannot be indexed, so it is surfaced rather than silently
// dropped into an index that no longer matches the active set.
func TestInitGenesisRejectsDanglingActiveBallotID(t *testing.T) {
	f := SetupTest(t)
	err := f.k.InitGenesis(f.ctx, &types.GenesisState{
		Params:          types.Params{Admin: f.addrs[0].String()},
		ActiveBallotIds: []string{"ghost"},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "ghost")
}
