package integrationtest

import (
	"fmt"
	"testing"
	"time"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authz "github.com/cosmos/cosmos-sdk/x/authz"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/app"
	utils "github.com/pushchain/push-chain-node/testutils"
	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
	uregistrytypes "github.com/pushchain/push-chain-node/x/uregistry/types"
	uvalidatortypes "github.com/pushchain/push-chain-node/x/uvalidator/types"
)

func setupVoteGasPriceTest(t *testing.T, numVals int) (*app.ChainApp, sdk.Context, []string, []stakingtypes.Validator) {
	app, ctx, _, validators := utils.SetAppWithMultipleValidators(t, numVals)

	// --- Add chain config ---
	chainConfig := uregistrytypes.ChainConfig{
		Chain:          "eip155:11155111",
		VmType:         uregistrytypes.VmType_EVM,
		PublicRpcUrl:   "https://sepolia.drpc.org",
		GatewayAddress: "0x28E0F09bE2321c1420Dc60Ee146aACbD68B335Fe",
		Enabled: &uregistrytypes.ChainEnabled{
			IsInboundEnabled:  true,
			IsOutboundEnabled: true,
		},
	}
	require.NoError(t, app.UregistryKeeper.AddChainConfig(ctx, &chainConfig))

	// --- Register validators as universal validators ---
	universalVals := make([]string, len(validators))
	for i, val := range validators {
		pubkey := fmt.Sprintf("pubkey-%d", i)
		network := uvalidatortypes.NetworkInfo{Ip: fmt.Sprintf("192.168.0.%d", i+1)}

		require.NoError(t, app.UvalidatorKeeper.AddUniversalValidator(ctx, val.OperatorAddress, pubkey, network))
		universalVals[i] = sdk.AccAddress([]byte(fmt.Sprintf("universal-validator-%d", i))).String()
	}

	// --- Grant MsgVoteGasPrice authz ---
	for i, val := range validators {
		accAddr, err := sdk.ValAddressFromBech32(val.OperatorAddress)
		require.NoError(t, err)
		coreAcc := sdk.AccAddress(accAddr)

		uniAcc := sdk.MustAccAddressFromBech32(universalVals[i])
		auth := authz.NewGenericAuthorization(sdk.MsgTypeURL(&uexecutortypes.MsgVoteGasPrice{}))
		exp := ctx.BlockTime().Add(time.Hour)
		require.NoError(t, app.AuthzKeeper.SaveGrant(ctx, uniAcc, coreAcc, auth, &exp))
	}

	return app, ctx, universalVals, validators
}

func TestVoteGasPriceIntegration(t *testing.T) {
	t.Parallel()
	chainId := "eip155:11155111"

	t.Run("multiple validators vote and median calculated", func(t *testing.T) {
		app, ctx, uvals, vals := setupVoteGasPriceTest(t, 4)

		votes := []struct {
			uniVal string
			price  uint64
			block  uint64
		}{
			{uvals[0], 300_000_000_000, 12345},
			{uvals[1], 200_000_000_000, 12346},
			{uvals[2], 400_000_000_000, 12347},
			{uvals[3], 250_000_000_000, 12348},
		}

		for i, v := range votes {
			coreVal, err := sdk.ValAddressFromBech32(vals[i].OperatorAddress)
			require.NoError(t, err)
			coreAcc := sdk.AccAddress(coreVal).String()

			err = utils.ExecVoteGasPrice(t, ctx, app, v.uniVal, coreAcc, chainId, v.price, v.block)
			require.NoError(t, err)
		}

		stored, found, err := app.UexecutorKeeper.GetGasPrice(ctx, chainId)
		require.NoError(t, err)
		require.True(t, found)
		require.Len(t, stored.Prices, 4)

		median := stored.Prices[stored.MedianIndex]
		require.Contains(t, stored.Prices, median)
		require.Equal(t, uint64(300_000_000_000), median, "expected median to be the smaller price")

		medianBig := math.NewUint(median).BigInt()
		require.NotNil(t, medianBig)
	})

	t.Run("update existing vote", func(t *testing.T) {
		app, ctx, uvals, vals := setupVoteGasPriceTest(t, 1)

		coreVal, err := sdk.ValAddressFromBech32(vals[0].OperatorAddress)
		require.NoError(t, err)
		coreAcc := sdk.AccAddress(coreVal).String()

		// First vote
		require.NoError(t, utils.ExecVoteGasPrice(t, ctx, app, uvals[0], coreAcc, chainId, 100_000_000_000, 12345))
		// Update vote
		require.NoError(t, utils.ExecVoteGasPrice(t, ctx, app, uvals[0], coreAcc, chainId, 400_000_000_000, 12346))

		stored, found, err := app.UexecutorKeeper.GetGasPrice(ctx, chainId)
		require.NoError(t, err)
		require.True(t, found)
		require.Len(t, stored.Prices, 1)
		require.Equal(t, uint64(400_000_000_000), stored.Prices[0])
	})

	t.Run("all votes equal", func(t *testing.T) {
		app, ctx, uvals, vals := setupVoteGasPriceTest(t, 3)
		prices := []uint64{100_000_000_000, 100_000_000_000, 100_000_000_000}

		for i, v := range prices {
			coreVal, _ := sdk.ValAddressFromBech32(vals[i].OperatorAddress)
			coreAcc := sdk.AccAddress(coreVal).String()
			require.NoError(t, utils.ExecVoteGasPrice(t, ctx, app, uvals[i], coreAcc, chainId, v, uint64(i+1)))
		}

		stored, _, _ := app.UexecutorKeeper.GetGasPrice(ctx, chainId)
		median := stored.Prices[stored.MedianIndex]
		require.Equal(t, uint64(100_000_000_000), median)
	})

	t.Run("odd number of votes median", func(t *testing.T) {
		app, ctx, uvals, vals := setupVoteGasPriceTest(t, 3)
		votes := []uint64{100, 300, 200} // unordered

		for i, v := range votes {
			coreVal, _ := sdk.ValAddressFromBech32(vals[i].OperatorAddress)
			coreAcc := sdk.AccAddress(coreVal).String()
			require.NoError(t, utils.ExecVoteGasPrice(t, ctx, app, uvals[i], coreAcc, chainId, v, uint64(i+1)))
		}

		stored, _, _ := app.UexecutorKeeper.GetGasPrice(ctx, chainId)
		median := stored.Prices[stored.MedianIndex]
		require.Equal(t, uint64(200), median)
	})
}
