package integrationtest

import (
	"testing"

	abci "github.com/cometbft/cometbft/abci/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/app"
	utils "github.com/pushchain/push-chain-node/test/utils"
	uvalidatormodule "github.com/pushchain/push-chain-node/x/uvalidator"
	uvalidatortypes "github.com/pushchain/push-chain-node/x/uvalidator/types"
)

// buildVoteInfos constructs abci.VoteInfo entries for the given validators and powers.
func buildVoteInfos(t *testing.T, validators []stakingtypes.Validator, powers []int64) []abci.VoteInfo {
	t.Helper()
	require.Equal(t, len(validators), len(powers), "validators and powers must be same length")

	voteInfos := make([]abci.VoteInfo, len(validators))
	for i, val := range validators {
		consAddr, err := val.GetConsAddr()
		require.NoError(t, err)
		voteInfos[i] = abci.VoteInfo{
			Validator: abci.Validator{
				Address: consAddr,
				Power:   powers[i],
			},
		}
	}
	return voteInfos
}

// setActiveUV registers a validator as an active universal validator.
func setActiveUV(t *testing.T, chainApp *app.ChainApp, ctx sdk.Context, val stakingtypes.Validator) {
	t.Helper()
	valAddr, err := sdk.ValAddressFromBech32(val.OperatorAddress)
	require.NoError(t, err)

	uv := uvalidatortypes.UniversalValidator{
		IdentifyInfo: &uvalidatortypes.IdentityInfo{CoreValidatorAddress: val.OperatorAddress},
		LifecycleInfo: &uvalidatortypes.LifecycleInfo{
			CurrentStatus: uvalidatortypes.UVStatus_UV_STATUS_ACTIVE,
		},
		NetworkInfo: &uvalidatortypes.NetworkInfo{PeerId: "test", MultiAddrs: []string{"test"}},
	}
	require.NoError(t, chainApp.UvalidatorKeeper.UniversalValidatorSet.Set(ctx, valAddr, uv))
}

// fundFeeCollector mints and deposits coins into the fee collector module account.
func fundFeeCollector(t *testing.T, chainApp *app.ChainApp, ctx sdk.Context, amount int64) {
	t.Helper()
	coins := sdk.NewCoins(sdk.NewInt64Coin("upc", amount))
	err := chainApp.BankKeeper.MintCoins(ctx, utils.MintModule, coins)
	require.NoError(t, err)
	err = chainApp.BankKeeper.SendCoinsFromModuleToModule(ctx, utils.MintModule, authtypes.FeeCollectorName, coins)
	require.NoError(t, err)
}

func TestAllocateTokens(t *testing.T) {
	t.Run("no UVs: uvalidator balance is zero and all fees return to FeeCollector", func(t *testing.T) {
		chainApp, ctx, _, validators := utils.SetAppWithMultipleValidators(t, 3)

		const feeAmount = 1_000_000
		fundFeeCollector(t, chainApp, ctx, feeAmount)

		powers := []int64{100, 100, 100}
		voteInfos := buildVoteInfos(t, validators, powers)

		err := uvalidatormodule.AllocateTokens(ctx, 300, voteInfos, chainApp.UvalidatorKeeper)
		require.NoError(t, err)

		uvAddr := chainApp.AccountKeeper.GetModuleAddress(uvalidatortypes.ModuleName)
		require.True(t, chainApp.BankKeeper.GetAllBalances(ctx, uvAddr).IsZero(),
			"uvalidator module must have zero balance when there are no UVs")

		feeCollectorAddr := chainApp.AccountKeeper.GetModuleAddress(authtypes.FeeCollectorName)
		feeCollectorBalance := chainApp.BankKeeper.GetAllBalances(ctx, feeCollectorAddr)
		require.Equal(t, int64(feeAmount), feeCollectorBalance.AmountOf("upc").Int64(),
			"all fees must be returned to FeeCollector when there are no UVs")
	})

	t.Run("with one UV: extraCoins go to distribution, uvalidator balance is zero", func(t *testing.T) {
		chainApp, ctx, _, validators := utils.SetAppWithMultipleValidators(t, 3)

		setActiveUV(t, chainApp, ctx, validators[0])

		const feeAmount = 1_000_000
		fundFeeCollector(t, chainApp, ctx, feeAmount)

		distrAddr := chainApp.AccountKeeper.GetModuleAddress(distrtypes.ModuleName)
		distrBefore := chainApp.BankKeeper.GetAllBalances(ctx, distrAddr).AmountOf("upc")

		powers := []int64{100, 100, 100}
		voteInfos := buildVoteInfos(t, validators, powers)

		err := uvalidatormodule.AllocateTokens(ctx, 300, voteInfos, chainApp.UvalidatorKeeper)
		require.NoError(t, err)

		// uvalidator must be empty — no coins stuck
		uvAddr := chainApp.AccountKeeper.GetModuleAddress(uvalidatortypes.ModuleName)
		require.True(t, chainApp.BankKeeper.GetAllBalances(ctx, uvAddr).IsZero(),
			"uvalidator module must have zero balance after AllocateTokens")

		// distribution must have received the UV boost coins (extraCoins > 0)
		distrAfter := chainApp.BankKeeper.GetAllBalances(ctx, distrAddr).AmountOf("upc")
		distrGain := distrAfter.Sub(distrBefore)
		require.True(t, distrGain.IsPositive(),
			"distribution module must have received UV boost coins")

		// coins are conserved: distrGain + FeeCollector = original feeAmount
		feeCollectorAddr := chainApp.AccountKeeper.GetModuleAddress(authtypes.FeeCollectorName)
		feeCollectorBalance := chainApp.BankKeeper.GetAllBalances(ctx, feeCollectorAddr).AmountOf("upc")
		require.Equal(t, int64(feeAmount), distrGain.Add(feeCollectorBalance).Int64(),
			"coins must be conserved: extraCoins + remainingFinal must equal feesCollectedInt")
	})

	t.Run("multiple UVs: truncation dust never sticks in uvalidator", func(t *testing.T) {
		chainApp, ctx, _, validators := utils.SetAppWithMultipleValidators(t, 4)

		// Two active UVs with different powers to maximise rounding surface
		setActiveUV(t, chainApp, ctx, validators[0])
		setActiveUV(t, chainApp, ctx, validators[1])

		const feeAmount = 999 // intentionally odd to amplify rounding edge cases
		fundFeeCollector(t, chainApp, ctx, feeAmount)

		distrAddr := chainApp.AccountKeeper.GetModuleAddress(distrtypes.ModuleName)
		distrBefore := chainApp.BankKeeper.GetAllBalances(ctx, distrAddr).AmountOf("upc")

		powers := []int64{100, 200, 150, 50}
		voteInfos := buildVoteInfos(t, validators, powers)

		err := uvalidatormodule.AllocateTokens(ctx, 500, voteInfos, chainApp.UvalidatorKeeper)
		require.NoError(t, err)

		// uvalidator must be empty
		uvAddr := chainApp.AccountKeeper.GetModuleAddress(uvalidatortypes.ModuleName)
		require.True(t, chainApp.BankKeeper.GetAllBalances(ctx, uvAddr).IsZero(),
			"uvalidator module must have zero balance with multiple UVs")

		// coins are conserved across distribution + FeeCollector
		distrAfter := chainApp.BankKeeper.GetAllBalances(ctx, distrAddr).AmountOf("upc")
		distrGain := distrAfter.Sub(distrBefore)

		feeCollectorAddr := chainApp.AccountKeeper.GetModuleAddress(authtypes.FeeCollectorName)
		feeCollectorBalance := chainApp.BankKeeper.GetAllBalances(ctx, feeCollectorAddr).AmountOf("upc")

		require.Equal(t, int64(feeAmount), distrGain.Add(feeCollectorBalance).Int64(),
			"coins must be conserved with multiple UVs")
	})

	t.Run("empty FeeCollector: AllocateTokens is a no-op and returns no error", func(t *testing.T) {
		chainApp, ctx, _, validators := utils.SetAppWithMultipleValidators(t, 2)
		setActiveUV(t, chainApp, ctx, validators[0])

		// Do not fund FeeCollector — it is empty
		powers := []int64{100, 100}
		voteInfos := buildVoteInfos(t, validators, powers)

		err := uvalidatormodule.AllocateTokens(ctx, 200, voteInfos, chainApp.UvalidatorKeeper)
		require.NoError(t, err)

		uvAddr := chainApp.AccountKeeper.GetModuleAddress(uvalidatortypes.ModuleName)
		require.True(t, chainApp.BankKeeper.GetAllBalances(ctx, uvAddr).IsZero())
	})

	t.Run("all validators are UVs: all boost goes to distribution, uvalidator stays zero", func(t *testing.T) {
		chainApp, ctx, _, validators := utils.SetAppWithMultipleValidators(t, 3)

		for _, val := range validators {
			setActiveUV(t, chainApp, ctx, val)
		}

		const feeAmount = 500_000
		fundFeeCollector(t, chainApp, ctx, feeAmount)

		distrAddr := chainApp.AccountKeeper.GetModuleAddress(distrtypes.ModuleName)
		distrBefore := chainApp.BankKeeper.GetAllBalances(ctx, distrAddr).AmountOf("upc")

		powers := []int64{100, 100, 100}
		voteInfos := buildVoteInfos(t, validators, powers)

		err := uvalidatormodule.AllocateTokens(ctx, 300, voteInfos, chainApp.UvalidatorKeeper)
		require.NoError(t, err)

		uvAddr := chainApp.AccountKeeper.GetModuleAddress(uvalidatortypes.ModuleName)
		require.True(t, chainApp.BankKeeper.GetAllBalances(ctx, uvAddr).IsZero(),
			"uvalidator must be empty even when all validators are UVs")

		distrAfter := chainApp.BankKeeper.GetAllBalances(ctx, distrAddr).AmountOf("upc")
		distrGain := distrAfter.Sub(distrBefore)

		feeCollectorAddr := chainApp.AccountKeeper.GetModuleAddress(authtypes.FeeCollectorName)
		feeCollectorBalance := chainApp.BankKeeper.GetAllBalances(ctx, feeCollectorAddr).AmountOf("upc")

		require.Equal(t, int64(feeAmount), distrGain.Add(feeCollectorBalance).Int64(),
			"coins must be conserved when all validators are UVs")
	})
}
