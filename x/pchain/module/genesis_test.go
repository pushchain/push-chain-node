package pchain_test

import (
	"testing"

	keepertest "pchain/testutil/keeper"
	"pchain/testutil/nullify"
	pchain "pchain/x/pchain/module"
	"pchain/x/pchain/types"

	"github.com/stretchr/testify/require"
)

func TestGenesis(t *testing.T) {
	genesisState := types.GenesisState{
		Params: types.DefaultParams(),

		// this line is used by starport scaffolding # genesis/test/state
	}

	k, ctx := keepertest.PchainKeeper(t)
	pchain.InitGenesis(ctx, k, genesisState)
	got := pchain.ExportGenesis(ctx, k)
	require.NotNil(t, got)

	nullify.Fill(&genesisState)
	nullify.Fill(got)

	// this line is used by starport scaffolding # genesis/test/assert
}
