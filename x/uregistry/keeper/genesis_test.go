package keeper_test

import (
	"testing"

	"github.com/pushchain/push-chain-node/x/uregistry/types"
	"github.com/stretchr/testify/require"
)

func TestGenesis(t *testing.T) {
	f := SetupTest(t)

	// Use Exported=true to skip contract deployment (no real EVM keeper in unit tests)
	genesisState := &types.GenesisState{
		Params:   types.DefaultParams(),
		Exported: true,
	}
	f.k.InitGenesis(f.ctx, genesisState)

	got := f.k.ExportGenesis(f.ctx)
	require.NotNil(t, got)
}

func TestGenesisExportImportRoundTrip(t *testing.T) {
	f := SetupTest(t)
	f.k.InitGenesis(f.ctx, &types.GenesisState{Params: types.DefaultParams(), Exported: true})

	// Populate state: ChainConfigs
	chainConfig := types.ChainConfig{
		Chain:          "eip155:1",
		PublicRpcUrl:   "https://eth.rpc",
		GatewayAddress: "0xgateway",
	}
	require.NoError(t, f.k.ChainConfigs.Set(f.ctx, "eip155:1", chainConfig))

	// Populate state: TokenConfigs
	tokenConfig := types.TokenConfig{
		Chain:   "eip155:1",
		Address: "0xtoken",
		Name:    "TestToken",
		Symbol:  "TT",
	}
	require.NoError(t, f.k.TokenConfigs.Set(f.ctx, "eip155:1:0xtoken", tokenConfig))

	// Export
	exported := f.k.ExportGenesis(f.ctx)
	require.NotNil(t, exported)
	require.True(t, exported.Exported)
	require.Len(t, exported.ChainConfigs, 1)
	require.Len(t, exported.TokenConfigs, 1)
	require.Equal(t, "eip155:1", exported.ChainConfigs[0].Key)
	require.Equal(t, "eip155:1:0xtoken", exported.TokenConfigs[0].Key)

	// Re-init on fresh fixture with exported state
	f2 := SetupTest(t)
	f2.k.InitGenesis(f2.ctx, exported)

	// Export again and compare
	reExported := f2.k.ExportGenesis(f2.ctx)
	require.Equal(t, len(exported.ChainConfigs), len(reExported.ChainConfigs))
	require.Equal(t, len(exported.TokenConfigs), len(reExported.TokenConfigs))
	require.Equal(t, exported.ChainConfigs[0].Key, reExported.ChainConfigs[0].Key)
	require.Equal(t, exported.ChainConfigs[0].Value.Chain, reExported.ChainConfigs[0].Value.Chain)
	require.Equal(t, exported.TokenConfigs[0].Key, reExported.TokenConfigs[0].Key)
	require.Equal(t, exported.TokenConfigs[0].Value.Symbol, reExported.TokenConfigs[0].Value.Symbol)
}

func TestGenesisExportedSkipsDeployment(t *testing.T) {
	f := SetupTest(t)

	exported := &types.GenesisState{
		Params:   types.DefaultParams(),
		Exported: true,
	}
	// Should not panic — Exported=true skips contract deployment
	f.k.InitGenesis(f.ctx, exported)

	got := f.k.ExportGenesis(f.ctx)
	require.NotNil(t, got)
}
