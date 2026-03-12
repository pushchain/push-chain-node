package integrationtest

import (
	"fmt"
	"math/big"
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authz "github.com/cosmos/cosmos-sdk/x/authz"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/app"
	utils "github.com/pushchain/push-chain-node/test/utils"
	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
	uregistrytypes "github.com/pushchain/push-chain-node/x/uregistry/types"
	uvalidatortypes "github.com/pushchain/push-chain-node/x/uvalidator/types"
)

func setupVoteChainMetaTest(t *testing.T, numVals int) (*app.ChainApp, sdk.Context, []string, []stakingtypes.Validator) {
	testApp, ctx, _, validators := utils.SetAppWithMultipleValidators(t, numVals)

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
	require.NoError(t, testApp.UregistryKeeper.AddChainConfig(ctx, &chainConfig))

	universalVals := make([]string, len(validators))
	for i, val := range validators {
		network := uvalidatortypes.NetworkInfo{PeerId: fmt.Sprintf("temp%d", i+1), MultiAddrs: []string{"temp"}}
		require.NoError(t, testApp.UvalidatorKeeper.AddUniversalValidator(ctx, val.OperatorAddress, network))
		universalVals[i] = sdk.AccAddress([]byte(fmt.Sprintf("universal-validator-%d", i))).String()
	}

	for i, val := range validators {
		accAddr, err := sdk.ValAddressFromBech32(val.OperatorAddress)
		require.NoError(t, err)
		coreAcc := sdk.AccAddress(accAddr)
		uniAcc := sdk.MustAccAddressFromBech32(universalVals[i])
		auth := authz.NewGenericAuthorization(sdk.MsgTypeURL(&uexecutortypes.MsgVoteChainMeta{}))
		exp := ctx.BlockTime().Add(time.Hour)
		require.NoError(t, testApp.AuthzKeeper.SaveGrant(ctx, uniAcc, coreAcc, auth, &exp))
	}

	return testApp, ctx, universalVals, validators
}

func TestVoteChainMetaIntegration(t *testing.T) {
	t.Parallel()
	chainId := "eip155:11155111"

	t.Run("single validator vote stores chain meta", func(t *testing.T) {
		testApp, ctx, uvals, vals := setupVoteChainMetaTest(t, 1)

		coreVal, err := sdk.ValAddressFromBech32(vals[0].OperatorAddress)
		require.NoError(t, err)
		coreAcc := sdk.AccAddress(coreVal).String()

		err = utils.ExecVoteChainMeta(t, ctx, testApp, uvals[0], coreAcc, chainId, 100_000_000_000, 12345, 1700000000)
		require.NoError(t, err)

		stored, found, err := testApp.UexecutorKeeper.GetChainMeta(ctx, chainId)
		require.NoError(t, err)
		require.True(t, found)
		require.Len(t, stored.Prices, 1)
		require.Equal(t, uint64(100_000_000_000), stored.Prices[0])
		require.Len(t, stored.ChainHeights, 1)
		require.Equal(t, uint64(12345), stored.ChainHeights[0])
		require.Len(t, stored.ObservedAts, 1)
		require.Equal(t, uint64(1700000000), stored.ObservedAts[0])
	})

	t.Run("multiple validators vote and median calculated", func(t *testing.T) {
		testApp, ctx, uvals, vals := setupVoteChainMetaTest(t, 4)

		votes := []struct {
			uniVal     string
			price      uint64
			height     uint64
			observedAt uint64
		}{
			{uvals[0], 300_000_000_000, 12345, 1700000001},
			{uvals[1], 200_000_000_000, 12346, 1700000002},
			{uvals[2], 400_000_000_000, 12347, 1700000003},
			{uvals[3], 250_000_000_000, 12348, 1700000004},
		}

		for i, v := range votes {
			coreVal, err := sdk.ValAddressFromBech32(vals[i].OperatorAddress)
			require.NoError(t, err)
			coreAcc := sdk.AccAddress(coreVal).String()
			err = utils.ExecVoteChainMeta(t, ctx, testApp, v.uniVal, coreAcc, chainId, v.price, v.height, v.observedAt)
			require.NoError(t, err)
		}

		stored, found, err := testApp.UexecutorKeeper.GetChainMeta(ctx, chainId)
		require.NoError(t, err)
		require.True(t, found)
		require.Len(t, stored.Prices, 4)
		require.Len(t, stored.ChainHeights, 4)
		require.Len(t, stored.ObservedAts, 4)

		median := stored.Prices[stored.MedianIndex]
		// sorted: 200, 250, 300, 400 → median at index 2 → 300
		require.Equal(t, uint64(300_000_000_000), median)
	})

	t.Run("update existing vote", func(t *testing.T) {
		testApp, ctx, uvals, vals := setupVoteChainMetaTest(t, 1)

		coreVal, err := sdk.ValAddressFromBech32(vals[0].OperatorAddress)
		require.NoError(t, err)
		coreAcc := sdk.AccAddress(coreVal).String()

		require.NoError(t, utils.ExecVoteChainMeta(t, ctx, testApp, uvals[0], coreAcc, chainId, 100_000_000_000, 12345, 1700000000))
		// Update same validator's vote
		require.NoError(t, utils.ExecVoteChainMeta(t, ctx, testApp, uvals[0], coreAcc, chainId, 400_000_000_000, 12350, 1700000100))

		stored, found, err := testApp.UexecutorKeeper.GetChainMeta(ctx, chainId)
		require.NoError(t, err)
		require.True(t, found)
		require.Len(t, stored.Prices, 1, "should have only one entry (updated in-place)")
		require.Equal(t, uint64(400_000_000_000), stored.Prices[0])
		require.Equal(t, uint64(12350), stored.ChainHeights[0])
		require.Equal(t, uint64(1700000100), stored.ObservedAts[0])
	})

	t.Run("odd number of votes median", func(t *testing.T) {
		testApp, ctx, uvals, vals := setupVoteChainMetaTest(t, 3)
		prices := []uint64{100, 300, 200}

		for i, price := range prices {
			coreVal, _ := sdk.ValAddressFromBech32(vals[i].OperatorAddress)
			coreAcc := sdk.AccAddress(coreVal).String()
			require.NoError(t, utils.ExecVoteChainMeta(t, ctx, testApp, uvals[i], coreAcc, chainId, price, uint64(i+1), uint64(1700000000+i)))
		}

		stored, _, _ := testApp.UexecutorKeeper.GetChainMeta(ctx, chainId)
		median := stored.Prices[stored.MedianIndex]
		require.Equal(t, uint64(200), median)
	})
}

func TestMigrateGasPricesToChainMeta(t *testing.T) {
	chainId := "eip155:11155111"

	t.Run("migrates gas prices to chain meta", func(t *testing.T) {
		testApp, ctx, _, _ := utils.SetAppWithMultipleValidators(t, 1)

		// Seed a GasPrice entry manually (simulating pre-upgrade state)
		require.NoError(t, testApp.UexecutorKeeper.SetGasPrice(ctx, chainId, uexecutortypes.GasPrice{
			ObservedChainId: chainId,
			Signers:         []string{"cosmos1abc", "cosmos1def"},
			Prices:          []uint64{100_000_000_000, 200_000_000_000},
			BlockNums:       []uint64{12345, 12346},
			MedianIndex:     1,
		}))

		// Run migration
		require.NoError(t, testApp.UexecutorKeeper.MigrateGasPricesToChainMeta(ctx))

		// Verify ChainMeta was created
		cm, found, err := testApp.UexecutorKeeper.GetChainMeta(ctx, chainId)
		require.NoError(t, err)
		require.True(t, found)

		require.Equal(t, chainId, cm.ObservedChainId)
		require.Equal(t, []string{"cosmos1abc", "cosmos1def"}, cm.Signers)
		require.Equal(t, []uint64{100_000_000_000, 200_000_000_000}, cm.Prices)
		// block_nums from GasPrice become chain_heights in ChainMeta
		require.Equal(t, []uint64{12345, 12346}, cm.ChainHeights)
		// observedAts unknown at migration time → all 0
		require.Equal(t, []uint64{0, 0}, cm.ObservedAts)
		require.Equal(t, uint64(1), cm.MedianIndex)
	})

	t.Run("migration is idempotent: does not overwrite existing chain meta", func(t *testing.T) {
		testApp, ctx, _, _ := utils.SetAppWithMultipleValidators(t, 1)

		// Pre-seed a GasPrice entry
		require.NoError(t, testApp.UexecutorKeeper.SetGasPrice(ctx, chainId, uexecutortypes.GasPrice{
			ObservedChainId: chainId,
			Signers:         []string{"cosmos1abc"},
			Prices:          []uint64{100_000_000_000},
			BlockNums:       []uint64{12345},
			MedianIndex:     0,
		}))

		// Pre-seed a ChainMeta entry (simulating a validator already voted post-upgrade)
		require.NoError(t, testApp.UexecutorKeeper.SetChainMeta(ctx, chainId, uexecutortypes.ChainMeta{
			ObservedChainId: chainId,
			Signers:         []string{"cosmos1xyz"},
			Prices:          []uint64{999_000_000_000},
			ChainHeights:    []uint64{99999},
			ObservedAts:     []uint64{1700000099},
			MedianIndex:     0,
		}))

		// Run migration — should skip because ChainMeta already exists
		require.NoError(t, testApp.UexecutorKeeper.MigrateGasPricesToChainMeta(ctx))

		// Verify the existing entry was NOT overwritten
		cm, found, err := testApp.UexecutorKeeper.GetChainMeta(ctx, chainId)
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, []string{"cosmos1xyz"}, cm.Signers, "existing chain meta should not be overwritten")
		require.Equal(t, uint64(999_000_000_000), cm.Prices[0])
	})
}

// TestVoteChainMetaContractState verifies that after voting, the UniversalCore contract's
// on-chain storage reflects the correct gas price, chain height, and observed timestamp.
func TestVoteChainMetaContractState(t *testing.T) {
	chainId := "eip155:11155111"
	const (
		price      = uint64(100_000_000_000)
		height     = uint64(12345)
		observedAt = uint64(1700000000)
	)

	testApp, ctx, uvals, vals := setupVoteChainMetaTest(t, 1)

	coreVal, err := sdk.ValAddressFromBech32(vals[0].OperatorAddress)
	require.NoError(t, err)
	coreAcc := sdk.AccAddress(coreVal).String()

	require.NoError(t, utils.ExecVoteChainMeta(t, ctx, testApp, uvals[0], coreAcc, chainId, price, height, observedAt))

	// Read from the UniversalCore contract using the public mapping getters
	universalCoreAddr := utils.GetDefaultAddresses().HandlerAddr
	ucABI, err := uexecutortypes.ParseUniversalCoreABI()
	require.NoError(t, err)

	caller, _ := testApp.UexecutorKeeper.GetUeModuleAddress(ctx)

	t.Run("gasPriceByChainNamespace matches voted price", func(t *testing.T) {
		res, err := testApp.EVMKeeper.CallEVM(ctx, ucABI, caller, universalCoreAddr, false, "gasPriceByChainNamespace", chainId)
		require.NoError(t, err)
		got := new(big.Int).SetBytes(res.Ret)
		require.Equal(t, new(big.Int).SetUint64(price), got)
	})

	t.Run("chainHeightByChainNamespace matches voted height", func(t *testing.T) {
		res, err := testApp.EVMKeeper.CallEVM(ctx, ucABI, caller, universalCoreAddr, false, "chainHeightByChainNamespace", chainId)
		require.NoError(t, err)
		got := new(big.Int).SetBytes(res.Ret)
		require.Equal(t, new(big.Int).SetUint64(height), got)
	})

	t.Run("timestampObservedAtByChainNamespace matches voted observedAt", func(t *testing.T) {
		res, err := testApp.EVMKeeper.CallEVM(ctx, ucABI, caller, universalCoreAddr, false, "timestampObservedAtByChainNamespace", chainId)
		require.NoError(t, err)
		got := new(big.Int).SetBytes(res.Ret)
		require.Equal(t, new(big.Int).SetUint64(observedAt), got)
	})
}
