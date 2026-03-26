package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/pushchain/push-chain-node/x/uvalidator/types"
	"github.com/stretchr/testify/require"
)

func TestGenesis(t *testing.T) {
	f := SetupTest(t)

	genesisState := &types.GenesisState{
		Params: types.DefaultParams(),
	}

	f.k.InitGenesis(f.ctx, genesisState)

	got := f.k.ExportGenesis(f.ctx)
	require.NotNil(t, got)
}

func TestGenesisExportImportRoundTrip(t *testing.T) {
	f := SetupTest(t)
	f.k.InitGenesis(f.ctx, &types.GenesisState{Params: types.DefaultParams()})

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
