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

	t.Run("votes below bootstrap quorum store but do not bootstrap oracle", func(t *testing.T) {
		// With chainMetaMinVotesForFirstWrite = 3, votes 1 and 2 are recorded
		// in state but do NOT trigger an EVM oracle write. LastAppliedChainHeight
		// stays 0 until the third fresh vote accumulates.
		testApp, ctx, uvals, vals := setupVoteChainMetaTest(t, 2)

		coreAccs := make([]string, 2)
		for i := range vals {
			coreVal, _ := sdk.ValAddressFromBech32(vals[i].OperatorAddress)
			coreAccs[i] = sdk.AccAddress(coreVal).String()
		}

		// Vote 1
		require.NoError(t, utils.ExecVoteChainMeta(t, ctx, testApp, uvals[0], coreAccs[0], chainId, 100_000_000_000, 12345))
		stored, found, err := testApp.UexecutorKeeper.GetChainMeta(ctx, chainId)
		require.NoError(t, err)
		require.True(t, found)
		require.Len(t, stored.Prices, 1)
		require.Equal(t, uint64(0), stored.LastAppliedChainHeight, "single vote should not bootstrap the oracle")

		// Vote 2
		require.NoError(t, utils.ExecVoteChainMeta(t, ctx, testApp, uvals[1], coreAccs[1], chainId, 200_000_000_000, 12346))
		stored, _, _ = testApp.UexecutorKeeper.GetChainMeta(ctx, chainId)
		require.Len(t, stored.Prices, 2)
		require.Equal(t, uint64(0), stored.LastAppliedChainHeight, "two votes should still not bootstrap the oracle")
	})

	t.Run("third fresh vote bootstraps the oracle and sets LastAppliedChainHeight to median", func(t *testing.T) {
		testApp, ctx, uvals, vals := setupVoteChainMetaTest(t, 3)

		coreAccs := make([]string, 3)
		for i := range vals {
			coreVal, _ := sdk.ValAddressFromBech32(vals[i].OperatorAddress)
			coreAccs[i] = sdk.AccAddress(coreVal).String()
		}

		// First two votes — stored only, no EVM write yet
		require.NoError(t, utils.ExecVoteChainMeta(t, ctx, testApp, uvals[0], coreAccs[0], chainId, 100_000_000_000, 12345))
		require.NoError(t, utils.ExecVoteChainMeta(t, ctx, testApp, uvals[1], coreAccs[1], chainId, 300_000_000_000, 12346))

		stored, _, _ := testApp.UexecutorKeeper.GetChainMeta(ctx, chainId)
		require.Equal(t, uint64(0), stored.LastAppliedChainHeight)

		// Third vote — now ≥3 fresh votes, EVM write happens with the upper median.
		// Sorted prices  [100B, 200B, 300B] → upper median @ index 1 = 200B.
		// Sorted heights [12345, 12346, 12347] → upper median @ index 1 = 12346.
		require.NoError(t, utils.ExecVoteChainMeta(t, ctx, testApp, uvals[2], coreAccs[2], chainId, 200_000_000_000, 12347))
		stored, _, _ = testApp.UexecutorKeeper.GetChainMeta(ctx, chainId)
		require.Len(t, stored.Prices, 3)
		require.Equal(t, uint64(12346), stored.LastAppliedChainHeight)
	})

	t.Run("multiple validators vote and independent medians calculated", func(t *testing.T) {
		testApp, ctx, uvals, vals := setupVoteChainMetaTest(t, 4)

		// Heights must be strictly increasing to pass the lastApplied check on each vote.
		votes := []struct {
			uniVal string
			price  uint64
			height uint64
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
			err = utils.ExecVoteChainMeta(t, ctx, testApp, v.uniVal, coreAcc, chainId, v.price, v.height)
			require.NoError(t, err)
		}

		stored, found, err := testApp.UexecutorKeeper.GetChainMeta(ctx, chainId)
		require.NoError(t, err)
		require.True(t, found)
		require.Len(t, stored.Prices, 4)
		require.Len(t, stored.ChainHeights, 4)
		require.Len(t, stored.StoredAts, 4)

		// Price median: sorted [200B, 250B, 300B, 400B], upper median at index 2 = 300B
		medianPrice := stored.Prices[stored.MedianIndex]
		require.Equal(t, uint64(300_000_000_000), medianPrice)
	})

	t.Run("update existing vote", func(t *testing.T) {
		testApp, ctx, uvals, vals := setupVoteChainMetaTest(t, 1)

		coreVal, err := sdk.ValAddressFromBech32(vals[0].OperatorAddress)
		require.NoError(t, err)
		coreAcc := sdk.AccAddress(coreVal).String()

		require.NoError(t, utils.ExecVoteChainMeta(t, ctx, testApp, uvals[0], coreAcc, chainId, 100_000_000_000, 12345))
		// Update same validator's vote with a higher height
		require.NoError(t, utils.ExecVoteChainMeta(t, ctx, testApp, uvals[0], coreAcc, chainId, 400_000_000_000, 12350))

		stored, found, err := testApp.UexecutorKeeper.GetChainMeta(ctx, chainId)
		require.NoError(t, err)
		require.True(t, found)
		require.Len(t, stored.Prices, 1, "should have only one entry (updated in-place)")
		require.Equal(t, uint64(400_000_000_000), stored.Prices[0])
		require.Equal(t, uint64(12350), stored.ChainHeights[0])
	})

	t.Run("odd number of votes median", func(t *testing.T) {
		testApp, ctx, uvals, vals := setupVoteChainMetaTest(t, 3)

		// Heights: 1, 2, 3 — each strictly greater than lastApplied after previous vote.
		// After vote 1 (height=1): lastApplied=1.
		// After vote 2 (height=2): lastApplied=2 (median of [1,2]).
		// After vote 3 (height=3): median heights = [1,2,3] → upper median = 2. lastApplied=2.
		heights := []uint64{1, 2, 3}
		prices := []uint64{100, 300, 200}

		for i, price := range prices {
			coreVal, _ := sdk.ValAddressFromBech32(vals[i].OperatorAddress)
			coreAcc := sdk.AccAddress(coreVal).String()
			require.NoError(t, utils.ExecVoteChainMeta(t, ctx, testApp, uvals[i], coreAcc, chainId, price, heights[i]))
		}

		stored, _, _ := testApp.UexecutorKeeper.GetChainMeta(ctx, chainId)
		median := stored.Prices[stored.MedianIndex]
		// sorted prices: [100, 200, 300] → upper median at index 1 = 200
		require.Equal(t, uint64(200), median)
	})

	t.Run("vote rejected when chain height not greater than last applied", func(t *testing.T) {
		// Bootstrap requires 3 fresh votes before LastAppliedChainHeight is set,
		// so the height-staleness check only applies after all three validators have voted.
		testApp, ctx, uvals, vals := setupVoteChainMetaTest(t, 3)

		coreAccs := make([]string, 3)
		for i := range vals {
			coreVal, _ := sdk.ValAddressFromBech32(vals[i].OperatorAddress)
			coreAccs[i] = sdk.AccAddress(coreVal).String()
		}

		// Three votes to bootstrap — heights 99, 100, 101. Upper median @ index 1 = 100.
		require.NoError(t, utils.ExecVoteChainMeta(t, ctx, testApp, uvals[0], coreAccs[0], chainId, 100_000_000_000, 99))
		require.NoError(t, utils.ExecVoteChainMeta(t, ctx, testApp, uvals[1], coreAccs[1], chainId, 100_000_000_000, 100))
		require.NoError(t, utils.ExecVoteChainMeta(t, ctx, testApp, uvals[2], coreAccs[2], chainId, 100_000_000_000, 101))

		stored, _, _ := testApp.UexecutorKeeper.GetChainMeta(ctx, chainId)
		require.Equal(t, uint64(100), stored.LastAppliedChainHeight)

		// Same height → rejected
		err := utils.ExecVoteChainMeta(t, ctx, testApp, uvals[0], coreAccs[0], chainId, 200_000_000_000, 100)
		require.Error(t, err)
		require.Contains(t, err.Error(), "not greater than last applied chain height")

		// Lower height → rejected
		err = utils.ExecVoteChainMeta(t, ctx, testApp, uvals[0], coreAccs[0], chainId, 200_000_000_000, 99)
		require.Error(t, err)
		require.Contains(t, err.Error(), "not greater than last applied chain height")

		// Higher height → accepted
		require.NoError(t, utils.ExecVoteChainMeta(t, ctx, testApp, uvals[0], coreAccs[0], chainId, 200_000_000_000, 102))
	})

	t.Run("stale votes excluded from median", func(t *testing.T) {
		testApp, ctx, uvals, vals := setupVoteChainMetaTest(t, 3)

		coreAccs := make([]string, 3)
		for i := range vals {
			coreVal, _ := sdk.ValAddressFromBech32(vals[i].OperatorAddress)
			coreAccs[i] = sdk.AccAddress(coreVal).String()
		}

		// All 3 validators vote at T. Heights 1, 2, 3 to pass lastApplied checks.
		require.NoError(t, utils.ExecVoteChainMeta(t, ctx, testApp, uvals[0], coreAccs[0], chainId, 100, 1))
		require.NoError(t, utils.ExecVoteChainMeta(t, ctx, testApp, uvals[1], coreAccs[1], chainId, 300, 2))
		require.NoError(t, utils.ExecVoteChainMeta(t, ctx, testApp, uvals[2], coreAccs[2], chainId, 200, 3))

		// After all 3 votes at T: sorted prices [100,200,300], upper median=200. lastApplied = median height = 2.
		stored, _, _ := testApp.UexecutorKeeper.GetChainMeta(ctx, chainId)
		require.Equal(t, uint64(2), stored.LastAppliedChainHeight)

		// Advance block time by 301 seconds — old votes become stale.
		ctx = ctx.WithBlockTime(ctx.BlockTime().Add(301 * time.Second))

		// val0 re-votes with price=900, height=3 (> lastApplied=2).
		// Only this fresh vote contributes to the new median.
		require.NoError(t, utils.ExecVoteChainMeta(t, ctx, testApp, uvals[0], coreAccs[0], chainId, 900, 3))

		stored, found, err := testApp.UexecutorKeeper.GetChainMeta(ctx, chainId)
		require.NoError(t, err)
		require.True(t, found)
		// LastAppliedChainHeight should now be 3 (only fresh vote: height=3)
		require.Equal(t, uint64(3), stored.LastAppliedChainHeight)

		// Verify the applied price via EVM contract — only val0's fresh vote (900) should have been used,
		// not the stale votes (100, 300). MedianIndex on the stored entry reflects the full-slice
		// median, so we must query the contract directly for the actually-applied value.
		universalCoreAddr := utils.GetDefaultAddresses().HandlerAddr
		ucABI, err := uexecutortypes.ParseUniversalCoreABI()
		require.NoError(t, err)
		caller, _ := testApp.UexecutorKeeper.GetUeModuleAddress(ctx)
		res, err := testApp.EVMKeeper.CallEVM(ctx, ucABI, caller, universalCoreAddr, false, "gasPriceByChainNamespace", chainId)
		require.NoError(t, err)
		appliedPrice := new(big.Int).SetBytes(res.Ret)
		require.Equal(t, new(big.Int).SetUint64(900), appliedPrice, "stale votes must not influence the applied median price")
	})

	t.Run("last applied chain height updated after EVM call", func(t *testing.T) {
		// Bootstrap requires 3 fresh votes; the EVM write happens on the third
		// vote and LastAppliedChainHeight reflects the upper-median height.
		testApp, ctx, uvals, vals := setupVoteChainMetaTest(t, 3)

		coreAccs := make([]string, 3)
		for i := range vals {
			coreVal, _ := sdk.ValAddressFromBech32(vals[i].OperatorAddress)
			coreAccs[i] = sdk.AccAddress(coreVal).String()
		}

		require.NoError(t, utils.ExecVoteChainMeta(t, ctx, testApp, uvals[0], coreAccs[0], chainId, 100_000_000_000, 1000))
		require.NoError(t, utils.ExecVoteChainMeta(t, ctx, testApp, uvals[1], coreAccs[1], chainId, 200_000_000_000, 2000))
		// First two votes are stored-only — no EVM write, lastApplied stays 0.
		stored, _, _ := testApp.UexecutorKeeper.GetChainMeta(ctx, chainId)
		require.Equal(t, uint64(0), stored.LastAppliedChainHeight)

		// Third vote — EVM write triggers. Sorted heights [1000, 2000, 3000] → upper median = 2000.
		require.NoError(t, utils.ExecVoteChainMeta(t, ctx, testApp, uvals[2], coreAccs[2], chainId, 300_000_000_000, 3000))

		stored, found, err := testApp.UexecutorKeeper.GetChainMeta(ctx, chainId)
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, uint64(2000), stored.LastAppliedChainHeight)
	})
}

func TestMigrateGasPricesToChainMeta(t *testing.T) {
	chainId := "eip155:11155111"

	t.Run("migrates gas prices to chain meta with zero stored_ats", func(t *testing.T) {
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
		require.Equal(t, uint64(1), cm.MedianIndex)
		// StoredAts should be zero-filled (migrated votes are treated as stale until re-voted)
		require.Equal(t, []uint64{0, 0}, cm.StoredAts)
		require.Equal(t, uint64(0), cm.LastAppliedChainHeight)
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
			StoredAts:       []uint64{0},
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
// on-chain storage reflects the correct gas price and chain height.
// The contract records block.timestamp as the observed-at value (no longer passed by the caller).
func TestVoteChainMetaContractState(t *testing.T) {
	chainId := "eip155:11155111"
	const (
		price  = uint64(100_000_000_000)
		height = uint64(12345)
	)

	// Bootstrap requires chainMetaMinVotesForFirstWrite (3) fresh votes before
	// the EVM oracle is written. All validators submit identical price/height
	// so the upper median equals the voted values.
	testApp, ctx, uvals, vals := setupVoteChainMetaTest(t, 3)

	coreAccs := make([]string, 3)
	for i := range vals {
		coreVal, err := sdk.ValAddressFromBech32(vals[i].OperatorAddress)
		require.NoError(t, err)
		coreAccs[i] = sdk.AccAddress(coreVal).String()
	}

	// Three agreeing votes → median == voted values, oracle is written.
	require.NoError(t, utils.ExecVoteChainMeta(t, ctx, testApp, uvals[0], coreAccs[0], chainId, price, height))
	require.NoError(t, utils.ExecVoteChainMeta(t, ctx, testApp, uvals[1], coreAccs[1], chainId, price, height))
	require.NoError(t, utils.ExecVoteChainMeta(t, ctx, testApp, uvals[2], coreAccs[2], chainId, price, height))

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
}
