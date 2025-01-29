package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	keepertest "push/testutil/keeper"
	"push/x/push/types"
)

func TestGetParams(t *testing.T) {
	k, ctx := keepertest.PushKeeper(t)
	params := types.DefaultParams()

	require.NoError(t, k.SetParams(ctx, params))
	require.EqualValues(t, params, k.GetParams(ctx))
}
