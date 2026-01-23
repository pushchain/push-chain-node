package module

import (
	"context"
	"time"

	"cosmossdk.io/math"
	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/cosmos/cosmos-sdk/telemetry"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/pushchain/push-chain-node/x/uvalidator/keeper"
	"github.com/pushchain/push-chain-node/x/uvalidator/types"
)

// BoostMultiplier is the factor applied to UV power when calculating effective total power.
// This inflates the denominator so non-UVs get diluted.
const BoostMultiplier = "1.148"

// ExtraBoostPortion is the fractional part of the boost (0.148)
// used when allocating only the incremental boost to UVs.
const ExtraBoostPortion = "0.148"

func BeginBlocker(ctx sdk.Context, uvalidatorKeeper keeper.Keeper) error {
	defer telemetry.ModuleMeasureSince(types.ModuleName, time.Now(), telemetry.MetricKeyBeginBlocker)

	// determine the total power signing the block
	var previousTotalPower int64
	for _, voteInfo := range ctx.VoteInfos() {
		previousTotalPower += voteInfo.Validator.Power
	}

	// full amount will be allocated to community pool by distribution module itself
	if previousTotalPower == 0 {
		return nil
	}

	height := ctx.BlockHeight()
	if height > 1 {
		if err := AllocateTokens(ctx, previousTotalPower, ctx.VoteInfos(), uvalidatorKeeper); err != nil {
			return err
		}
	}

	return nil
}

// AllocateTokens performs reward and fee distribution to all validators based
// on the F1 fee distribution specification.
func AllocateTokens(ctx context.Context, totalPreviousPower int64, bondedVotes []abci.VoteInfo, k keeper.Keeper) error {
	// fetch and clear the collected fees for distribution, since this is
	// called in BeginBlock, collected fees will be from the previous block
	// (and distributed to the previous proposer)
	feeCollector := k.AuthKeeper.GetModuleAccount(ctx, authtypes.FeeCollectorName)
	feesCollectedInt := k.BankKeeper.GetAllBalances(ctx, feeCollector.GetAddress())
	if feesCollectedInt.IsZero() {
		return nil
	}
	feesCollected := sdk.NewDecCoinsFromCoins(feesCollectedInt...)

	// transfer collected fees to the uvalidator module account
	err := k.BankKeeper.SendCoinsFromModuleToModule(ctx, authtypes.FeeCollectorName, types.ModuleName, feesCollectedInt)
	if err != nil {
		return err
	}

	// First: calculate effective total power (standard + boost for UVs)
	effectiveTotalPower := math.LegacyZeroDec()
	for _, vote := range bondedVotes {
		validator, err := k.StakingKeeper.ValidatorByConsAddr(ctx, vote.Validator.Address)
		if err != nil {
			return err
		}

		isUV, err := k.IsActiveUniversalValidator(ctx, validator.GetOperator())
		if err != nil {
			return err
		}

		power := math.LegacyNewDec(vote.Validator.Power)
		if isUV {
			power = power.Mul(math.LegacyMustNewDecFromStr(BoostMultiplier))
		}

		effectiveTotalPower = effectiveTotalPower.Add(power)
	}

	// Allocate 0.148x to the UVs proportionally using effective power
	allocated := sdk.NewDecCoins()
	for _, vote := range bondedVotes {
		validator, err := k.StakingKeeper.ValidatorByConsAddr(ctx, vote.Validator.Address)
		if err != nil {
			return err
		}

		isUV, err := k.IsActiveUniversalValidator(ctx, validator.GetOperator())
		if err != nil {
			return err
		}

		if !isUV {
			continue
		}

		// Use only the extra portion for UV allocation
		power := math.LegacyNewDec(vote.Validator.Power)
		power = power.Mul(math.LegacyMustNewDecFromStr(ExtraBoostPortion))

		powerFraction := power.QuoTruncate(effectiveTotalPower)
		reward := feesCollected.MulDecTruncate(powerFraction)

		err = k.DistributionKeeper.AllocateTokensToValidator(ctx, validator, reward)
		if err != nil {
			return err
		}

		allocated = allocated.Add(reward...)
	}

	// Calculate and return remaining (including any truncation change)
	extraCoins, extraChange := allocated.TruncateDecimal()
	remainingCoins, _ := feesCollectedInt.SafeSub(extraCoins...)

	remainingDec := sdk.NewDecCoinsFromCoins(remainingCoins...)
	remainingDec = remainingDec.Add(extraChange...)
	remainingFinal, _ := remainingDec.TruncateDecimal()

	if !remainingFinal.IsZero() {
		err = k.BankKeeper.SendCoinsFromModuleToModule(ctx, types.ModuleName, authtypes.FeeCollectorName, remainingFinal)
		if err != nil {
			return err
		}
	}

	// Distribution's BeginBlocker will now run and allocate rest amount of fees proportionally to the voting power
	return nil
}
