package pushchain_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	keepertest "pushchain/testutil/keeper"
	"pushchain/testutil/nullify"
	"pushchain/x/pushchain"
	"pushchain/x/pushchain/types"
)

func TestGenesis(t *testing.T) {
	genesisState := types.GenesisState{
		Params: types.DefaultParams(),

		// this line is used by starport scaffolding # genesis/test/state
	}

	k, ctx := keepertest.PushchainKeeper(t)
	pushchain.InitGenesis(ctx, *k, genesisState)
	got := pushchain.ExportGenesis(ctx, *k)
	require.NotNil(t, got)

	nullify.Fill(&genesisState)
	nullify.Fill(got)

	// this line is used by starport scaffolding # genesis/test/assert
}
