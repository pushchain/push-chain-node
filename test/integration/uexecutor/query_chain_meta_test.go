package integrationtest

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	utils "github.com/pushchain/push-chain-node/test/utils"
	uexecutorkeeper "github.com/pushchain/push-chain-node/x/uexecutor/keeper"
	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
)

// TestQueryChainMeta tests the ChainMeta and AllChainMetas query handlers
func TestQueryChainMeta(t *testing.T) {
	chainId := "eip155:11155111"
	chainId2 := "eip155:1"

	t.Run("returns not found when chain meta absent", func(t *testing.T) {
		testApp, ctx, _, _ := utils.SetAppWithMultipleValidators(t, 1)

		querier := uexecutorkeeper.NewQuerier(testApp.UexecutorKeeper)
		_, err := querier.ChainMeta(ctx, &uexecutortypes.QueryChainMetaRequest{ChainId: chainId})
		require.Error(t, err)
		require.Equal(t, codes.NotFound, status.Code(err))
	})

	t.Run("returns error for empty chain_id", func(t *testing.T) {
		testApp, ctx, _, _ := utils.SetAppWithMultipleValidators(t, 1)

		querier := uexecutorkeeper.NewQuerier(testApp.UexecutorKeeper)
		_, err := querier.ChainMeta(ctx, &uexecutortypes.QueryChainMetaRequest{ChainId: ""})
		require.Error(t, err)
		require.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("returns chain meta after vote", func(t *testing.T) {
		testApp, ctx, uvals, vals := setupVoteChainMetaTest(t, 2)

		coreVal0, _ := sdk.ValAddressFromBech32(vals[0].OperatorAddress)
		coreVal1, _ := sdk.ValAddressFromBech32(vals[1].OperatorAddress)

		require.NoError(t, utils.ExecVoteChainMeta(t, ctx, testApp, uvals[0], sdk.AccAddress(coreVal0).String(), chainId, 100_000_000_000, 12345))
		require.NoError(t, utils.ExecVoteChainMeta(t, ctx, testApp, uvals[1], sdk.AccAddress(coreVal1).String(), chainId, 200_000_000_000, 12346))

		querier := uexecutorkeeper.NewQuerier(testApp.UexecutorKeeper)
		resp, err := querier.ChainMeta(ctx, &uexecutortypes.QueryChainMetaRequest{ChainId: chainId})
		require.NoError(t, err)
		require.NotNil(t, resp.ChainMeta)
		require.Equal(t, chainId, resp.ChainMeta.ObservedChainId)
		require.Len(t, resp.ChainMeta.Prices, 2)
		require.Len(t, resp.ChainMeta.ChainHeights, 2)
		require.Len(t, resp.ChainMeta.ChainHeights, 2)
	})

	t.Run("all chain metas returns empty when nothing stored", func(t *testing.T) {
		testApp, ctx, _, _ := utils.SetAppWithMultipleValidators(t, 1)

		querier := uexecutorkeeper.NewQuerier(testApp.UexecutorKeeper)
		resp, err := querier.AllChainMetas(ctx, &uexecutortypes.QueryAllChainMetasRequest{})
		require.NoError(t, err)
		require.Empty(t, resp.ChainMetas)
	})

	t.Run("all chain metas returns all entries", func(t *testing.T) {
		testApp, ctx, _, _ := utils.SetAppWithMultipleValidators(t, 1)

		require.NoError(t, testApp.UexecutorKeeper.SetChainMeta(ctx, chainId, uexecutortypes.ChainMeta{
			ObservedChainId: chainId,
			Signers:         []string{"cosmos1abc"},
			Prices:          []uint64{100},
			ChainHeights:    []uint64{1},
			MedianIndex:     0,
		}))
		require.NoError(t, testApp.UexecutorKeeper.SetChainMeta(ctx, chainId2, uexecutortypes.ChainMeta{
			ObservedChainId: chainId2,
			Signers:         []string{"cosmos1def"},
			Prices:          []uint64{200},
			ChainHeights:    []uint64{2},
			MedianIndex:     0,
		}))

		querier := uexecutorkeeper.NewQuerier(testApp.UexecutorKeeper)
		resp, err := querier.AllChainMetas(ctx, &uexecutortypes.QueryAllChainMetasRequest{})
		require.NoError(t, err)
		require.Len(t, resp.ChainMetas, 2)
	})
}

// TestQueryGasPriceFromChainMeta ensures the legacy GasPrice query routes through ChainMetas
func TestQueryGasPriceFromChainMeta(t *testing.T) {
	chainId := "eip155:11155111"

	t.Run("gas price query reads from chain metas", func(t *testing.T) {
		testApp, ctx, _, _ := utils.SetAppWithMultipleValidators(t, 1)

		require.NoError(t, testApp.UexecutorKeeper.SetChainMeta(ctx, chainId, uexecutortypes.ChainMeta{
			ObservedChainId: chainId,
			Signers:         []string{"cosmos1abc", "cosmos1def"},
			Prices:          []uint64{100_000_000_000, 200_000_000_000},
			ChainHeights:    []uint64{12345, 12346},
			MedianIndex:     1,
		}))

		querier := uexecutorkeeper.NewQuerier(testApp.UexecutorKeeper)
		resp, err := querier.GasPrice(ctx, &uexecutortypes.QueryGasPriceRequest{ChainId: chainId})
		require.NoError(t, err)
		require.NotNil(t, resp.GasPrice)
		require.Equal(t, chainId, resp.GasPrice.ObservedChainId)
		require.Equal(t, []uint64{100_000_000_000, 200_000_000_000}, resp.GasPrice.Prices)
		// ChainHeights should be mapped back to BlockNums for backward compat
		require.Equal(t, []uint64{12345, 12346}, resp.GasPrice.BlockNums)
		require.Equal(t, uint64(1), resp.GasPrice.MedianIndex)
	})

	t.Run("gas price query falls back to legacy store when chain meta absent", func(t *testing.T) {
		testApp, ctx, _, _ := utils.SetAppWithMultipleValidators(t, 1)

		require.NoError(t, testApp.UexecutorKeeper.SetGasPrice(ctx, chainId, uexecutortypes.GasPrice{
			ObservedChainId: chainId,
			Signers:         []string{"cosmos1abc"},
			Prices:          []uint64{50_000_000_000},
			BlockNums:       []uint64{9999},
			MedianIndex:     0,
		}))

		querier := uexecutorkeeper.NewQuerier(testApp.UexecutorKeeper)
		resp, err := querier.GasPrice(ctx, &uexecutortypes.QueryGasPriceRequest{ChainId: chainId})
		require.NoError(t, err)
		require.Equal(t, uint64(50_000_000_000), resp.GasPrice.Prices[0])
		require.Equal(t, uint64(9999), resp.GasPrice.BlockNums[0])
	})

	t.Run("all gas prices sources from chain metas", func(t *testing.T) {
		testApp, ctx, _, _ := utils.SetAppWithMultipleValidators(t, 1)

		require.NoError(t, testApp.UexecutorKeeper.SetChainMeta(ctx, chainId, uexecutortypes.ChainMeta{
			ObservedChainId: chainId,
			Prices:          []uint64{100},
			ChainHeights:    []uint64{1},
			MedianIndex:     0,
		}))

		querier := uexecutorkeeper.NewQuerier(testApp.UexecutorKeeper)
		resp, err := querier.AllGasPrices(ctx, &uexecutortypes.QueryAllGasPricesRequest{})
		require.NoError(t, err)
		require.Len(t, resp.GasPrices, 1)
		require.Equal(t, chainId, resp.GasPrices[0].ObservedChainId)
	})
}
