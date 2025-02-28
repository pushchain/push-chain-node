package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	testkeeper "pushchain/testutil/keeper"
	"pushchain/x/pushchain/types"
)

func TestGetParams(t *testing.T) {
	k, ctx := testkeeper.PushchainKeeper(t)
	params := types.DefaultParams()

	k.SetParams(ctx, params)

	require.EqualValues(t, params, k.GetParams(ctx))
}
