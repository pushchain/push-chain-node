package supplyslash15

import (
	"context"

	sdkmath "cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"
	upgradetypes "cosmossdk.io/x/upgrade/types"
	"github.com/ethereum/go-ethereum/common"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"

	"github.com/pushchain/push-chain-node/app/upgrades"
	pchaintypes "github.com/pushchain/push-chain-node/types"
	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
)

const UpgradeName = "supply-slash-1.5"

// targetBalanceUPC is 2000 PC expressed in upc (2000 * 10^18).
// Each address will be slashed down to exactly this amount.
var targetBalanceUPC = sdkmath.NewInt(2000).Mul(sdkmath.NewInt(1_000_000_000_000_000_000))

// SlashAddresses are the 4 EVM addresses that escaped the previous supply-slash
// upgrade. All spendable balance above 2000 PC will be burned from each.
var SlashAddresses = []string{
	"0x99bcebf44433e901597d9fcb16e799a4847519f6",
	"0x961b650b516e3857e231e97f374acd509dbac316",
	"0x0e0f3b17B2d23aE40e925144E2C00b6fdaAb48F4",
	"0x9cB7d539EA7F5EacE1aF055D15501231d76dcDF7",
}

// NewUpgrade constructs the upgrade definition.
func NewUpgrade() upgrades.Upgrade {
	return upgrades.Upgrade{
		UpgradeName:          UpgradeName,
		CreateUpgradeHandler: CreateUpgradeHandler,
		StoreUpgrades:        storetypes.StoreUpgrades{},
	}
}

// CreateUpgradeHandler burns all spendable balance above 2000 PC from each of
// the four addresses that were missed by the prior supply-slash upgrade.
//
// Burn amount per address = spendable_balance - 2000 * 10^18 upc.
// Addresses whose spendable balance is already ≤ 2000 PC are skipped.
func CreateUpgradeHandler(
	mm upgrades.ModuleManager,
	configurator module.Configurator,
	ak *upgrades.AppKeepers,
) upgradetypes.UpgradeHandler {
	return func(ctx context.Context, plan upgradetypes.Plan, fromVM module.VersionMap) (module.VersionMap, error) {
		sdkCtx := sdk.UnwrapSDKContext(ctx)
		sdkCtx.Logger().Info("Running upgrade", "name", UpgradeName)

		totalBurned, success, skipped, errCount := SlashAddressesToTarget(sdkCtx, ak.BankKeeper, SlashAddresses)
		sdkCtx.Logger().Info("supply-slash-1.5: upgrade complete",
			"total_burned_upc", totalBurned.String(),
			"success", success,
			"skipped", skipped,
			"errors", errCount,
		)

		return mm.RunMigrations(ctx, configurator, fromVM)
	}
}

// SlashAddressesToTarget burns all spendable upc above 2000 PC for each address
// in the provided list. Returns total burned and per-outcome counts.
//
// Entries are skipped (not counted as errors) when:
//   - spendable balance ≤ 2000 PC (already at or below target)
//
// Entries are counted as errors when:
//   - hex string is not a valid EVM address
//   - SendCoinsFromAccountToModule fails
//
// Panics if BurnCoins fails after SendCoinsFromAccountToModule succeeds, since
// that would leave supply accounting in an inconsistent state.
func SlashAddressesToTarget(
	ctx sdk.Context,
	bk bankkeeper.BaseKeeper,
	addresses []string,
) (totalBurned sdkmath.Int, success, skipped, errCount int) {
	totalBurned = sdkmath.ZeroInt()

	for _, hexAddr := range addresses {
		if !common.IsHexAddress(hexAddr) {
			ctx.Logger().Error("supply-slash-1.5: invalid hex address, skipping",
				"address", hexAddr)
			errCount++
			continue
		}

		addr := sdk.AccAddress(common.HexToAddress(hexAddr).Bytes())

		spendable := bk.SpendableCoins(ctx, addr)
		spendableUPC := spendable.AmountOf(pchaintypes.BaseDenom)

		if spendableUPC.LTE(targetBalanceUPC) {
			ctx.Logger().Info("supply-slash-1.5: balance at or below 2000 PC target, skipping",
				"address", hexAddr,
				"spendable_upc", spendableUPC.String(),
				"target_upc", targetBalanceUPC.String())
			skipped++
			continue
		}

		// Burn everything above 2000 PC.
		burnAmt := spendableUPC.Sub(targetBalanceUPC)
		burnCoins := sdk.NewCoins(sdk.NewCoin(pchaintypes.BaseDenom, burnAmt))

		// Two-step burn: send to uexecutor module (has Burner permission), then burn.
		if err := bk.SendCoinsFromAccountToModule(ctx, addr, uexecutortypes.ModuleName, burnCoins); err != nil {
			ctx.Logger().Error("supply-slash-1.5: SendCoinsFromAccountToModule failed, skipping",
				"address", hexAddr, "error", err.Error())
			errCount++
			continue
		}

		if err := bk.BurnCoins(ctx, uexecutortypes.ModuleName, burnCoins); err != nil {
			// Coins left the account but BurnCoins failed: supply accounting is broken.
			panic("supply-slash-1.5: BurnCoins failed after SendCoinsFromAccountToModule: " + err.Error())
		}

		totalBurned = totalBurned.Add(burnAmt)
		success++
		ctx.Logger().Info("supply-slash-1.5: burned",
			"address", hexAddr,
			"burned_upc", burnAmt.String(),
			"remaining_upc", targetBalanceUPC.String())
	}

	return totalBurned, success, skipped, errCount
}
