package module

import (
	"context"
	"time"

	"cosmossdk.io/math"
	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/cosmos/cosmos-sdk/telemetry"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	"github.com/pushchain/push-chain-node/x/uvalidator/keeper"
	"github.com/pushchain/push-chain-node/x/uvalidator/types"
)

// NOTE:
// This logic works well when community tax = 0.
// When community tax > 0, UVs effectively get boosted from the full fee amount,
// and community tax is only applied to the remaining (after UV boost allocation), reducing the community pool share.

// BoostMultiplier is the factor applied to UV power when calculating effective total power.
// This inflates the denominator so non-UVs get diluted.
const BoostMultiplier = "1.148"

// ExtraBoostPortion is the fractional part of the boost (0.148)
// used when allocating only the incremental boost to UVs.
const ExtraBoostPortion = "0.148"

func BeginBlocker(ctx sdk.Context, uvalidatorKeeper keeper.Keeper) error {
	defer telemetry.ModuleMeasureSince(types.ModuleName, time.Now(), telemetry.MetricKeyBeginBlocker)

	ctx.Logger().Debug("uvalidator BeginBlocker started", "block_height", ctx.BlockHeight())

	// determine the total power signing the block
	var previousTotalPower int64
	for _, voteInfo := range ctx.VoteInfos() {
		previousTotalPower += voteInfo.Validator.Power
	}

	// full amount will be allocated to community pool by distribution module itself
	if previousTotalPower == 0 {
		ctx.Logger().Debug("uvalidator BeginBlocker: no voting power, skipping token allocation", "block_height", ctx.BlockHeight())
		return nil
	}

	height := ctx.BlockHeight()
	if height > 1 {
		ctx.Logger().Info("uvalidator BeginBlocker: allocating tokens",
			"block_height", height,
			"total_previous_power", previousTotalPower,
			"vote_infos_count", len(ctx.VoteInfos()),
		)
		if err := AllocateTokens(ctx, previousTotalPower, ctx.VoteInfos(), uvalidatorKeeper); err != nil {
			ctx.Logger().Error("uvalidator BeginBlocker: token allocation failed",
				"block_height", height,
				"error", err.Error(),
			)
			return err
		}
		ctx.Logger().Info("uvalidator BeginBlocker: token allocation complete", "block_height", height)
	}

	return nil
}

// AllocateTokens performs reward and fee distribution to all validators based
// on the F1 fee distribution specification.
func AllocateTokens(ctx context.Context, totalPreviousPower int64, bondedVotes []abci.VoteInfo, k keeper.Keeper) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// fetch and clear the collected fees for distribution, since this is
	// called in BeginBlock, collected fees will be from the previous block
	// (and distributed to the previous proposer)
	feeCollector := k.AuthKeeper.GetModuleAccount(ctx, authtypes.FeeCollectorName)
	feesCollectedInt := k.BankKeeper.GetAllBalances(ctx, feeCollector.GetAddress())
	if feesCollectedInt.IsZero() {
		k.Logger().Debug("AllocateTokens: no fees collected, skipping", "block_height", sdkCtx.BlockHeight())
		return nil
	}
	feesCollected := sdk.NewDecCoinsFromCoins(feesCollectedInt...)

	k.Logger().Debug("AllocateTokens: fees collected",
		"block_height", sdkCtx.BlockHeight(),
		"fees", feesCollectedInt.String(),
		"bonded_votes", len(bondedVotes),
	)

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

	k.Logger().Debug("AllocateTokens: effective total power computed",
		"block_height", sdkCtx.BlockHeight(),
		"effective_total_power", effectiveTotalPower.String(),
	)

	// Allocate 0.148x to the UVs proportionally using effective power
	allocated := sdk.NewDecCoins()
	uvCount := 0
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

		uvCount++

		// Use only the extra portion for UV allocation
		power := math.LegacyNewDec(vote.Validator.Power)
		power = power.Mul(math.LegacyMustNewDecFromStr(ExtraBoostPortion))

		powerFraction := power.QuoTruncate(effectiveTotalPower)
		reward := feesCollected.MulDecTruncate(powerFraction)

		k.Logger().Debug("AllocateTokens: allocating UV boost reward",
			"validator", validator.GetOperator(),
			"power", vote.Validator.Power,
			"reward", reward.String(),
		)

		err = k.DistributionKeeper.AllocateTokensToValidator(ctx, validator, reward)
		if err != nil {
			return err
		}

		allocated = allocated.Add(reward...)
	}

	// Calculate and return remaining (including any truncation change)
	extraCoins, extraChange := allocated.TruncateDecimal()
	remainingCoins, hasNeg := feesCollectedInt.SafeSub(extraCoins...)
	if hasNeg {
		remainingCoins = sdk.NewCoins() // clamp to zero - truncation rounding edge case
	}

	remainingDec := sdk.NewDecCoinsFromCoins(remainingCoins...)
	remainingDec = remainingDec.Add(extraChange...)
	remainingFinal, _ := remainingDec.TruncateDecimal()

	k.Logger().Info("AllocateTokens: reward distribution summary",
		"block_height", sdkCtx.BlockHeight(),
		"uv_count", uvCount,
		"uv_boost_allocated", extraCoins.String(),
		"remaining_to_fee_collector", remainingFinal.String(),
	)

	// Send the integer portion of UV boost rewards to the distribution module
	// to back the AllocateTokensToValidator accounting entries made above.
	// Without this, extraCoins sit in the uvalidator account permanently with
	// no mechanism to withdraw them, while the distribution module is underfunded
	// by exactly this amount for UV reward payouts.
	if !extraCoins.IsZero() {
		err = k.BankKeeper.SendCoinsFromModuleToModule(ctx, types.ModuleName, distrtypes.ModuleName, extraCoins)
		if err != nil {
			return err
		}
	}

	if !remainingFinal.IsZero() {
		err = k.BankKeeper.SendCoinsFromModuleToModule(ctx, types.ModuleName, authtypes.FeeCollectorName, remainingFinal)
		if err != nil {
			return err
		}
	}

	// Distribution's BeginBlocker will now run and allocate rest amount of fees proportionally to the voting power
	return nil
}
