package push_test

import (
	"testing"

	keepertest "push/testutil/keeper"
	"push/testutil/nullify"
	push "push/x/push/module"
	"push/x/push/types"

	"github.com/stretchr/testify/require"
)

func TestGenesis(t *testing.T) {
	genesisState := types.GenesisState{
		Params: types.DefaultParams(),

		// this line is used by starport scaffolding # genesis/test/state
	}

	k, ctx := keepertest.PushKeeper(t)
	push.InitGenesis(ctx, k, genesisState)
	got := push.ExportGenesis(ctx, k)
	require.NotNil(t, got)

	nullify.Fill(&genesisState)
	nullify.Fill(got)

	// this line is used by starport scaffolding # genesis/test/assert
}
