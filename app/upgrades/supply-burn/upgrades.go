package supplyburn

import (
	"context"
	_ "embed"
	"encoding/json"

	sdkmath "cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"
	upgradetypes "cosmossdk.io/x/upgrade/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"

	"github.com/pushchain/push-chain-node/app/upgrades"
	pchaintypes "github.com/pushchain/push-chain-node/types"
	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
)

const UpgradeName = "supply-burn"

// BurnEntry specifies one address and how much PC to keep (in whole PC tokens).
// The handler burns: spendable_balance - keep_pc * 10^18 upc.
// Set keep_pc = 0 to burn the entire spendable balance.
type BurnEntry struct {
	Address string `json:"address"`
	KeepPC  int64  `json:"keep_pc"` // whole PC to keep liquid (0 = burn all spendable)
}

//go:embed burn_targets.json
var burnTargetsJSON []byte

// NewUpgrade constructs the upgrade definition.
func NewUpgrade() upgrades.Upgrade {
	return upgrades.Upgrade{
		UpgradeName:          UpgradeName,
		CreateUpgradeHandler: CreateUpgradeHandler,
		StoreUpgrades:        storetypes.StoreUpgrades{},
	}
}

// CreateUpgradeHandler burns spendable balances of all addresses in burn_targets.json,
// keeping keep_pc * 10^18 upc per address.
// Burn amount is computed from the live spendable balance at upgrade time —
// not from a snapshot — so it works correctly regardless of accrued rewards.
func CreateUpgradeHandler(
	mm upgrades.ModuleManager,
	configurator module.Configurator,
	ak *upgrades.AppKeepers,
) upgradetypes.UpgradeHandler {
	return func(ctx context.Context, plan upgradetypes.Plan, fromVM module.VersionMap) (module.VersionMap, error) {
		sdkCtx := sdk.UnwrapSDKContext(ctx)
		sdkCtx.Logger().Info("Running upgrade", "name", UpgradeName)

		var entries []BurnEntry
		if err := json.Unmarshal(burnTargetsJSON, &entries); err != nil {
			panic("supply-burn: failed to parse burn_targets.json: " + err.Error())
		}

		totalBurned, success, skipped, errCount := ExecuteBurnEntries(sdkCtx, ak.BankKeeper, entries)
		sdkCtx.Logger().Info("supply-burn: upgrade complete",
			"total_burned_upc", totalBurned.String(),
			"success", success,
			"skipped", skipped,
			"errors", errCount,
		)

		return mm.RunMigrations(ctx, configurator, fromVM)
	}
}

// ExecuteBurnEntries burns (spendable_balance - keep_pc * 10^18) upc from each address.
// Returns total burned and per-outcome counts.
//
// Entries are skipped (not errors) when:
//   - spendable balance <= keep amount (nothing to burn)
//
// Entries are counted as errors when:
//   - address fails bech32 decoding
//
// Panics if BurnCoins fails after SendCoinsFromAccountToModule succeeds.
func ExecuteBurnEntries(
	ctx sdk.Context,
	bk bankkeeper.BaseKeeper,
	entries []BurnEntry,
) (totalBurned sdkmath.Int, success, skipped, errCount int) {
	totalBurned = sdkmath.ZeroInt()
	upcPerPC := sdkmath.NewInt(1_000_000_000_000_000_000)

	for _, entry := range entries {
		addr, err := sdk.AccAddressFromBech32(entry.Address)
		if err != nil {
			ctx.Logger().Error("supply-burn: invalid address, skipping",
				"address", entry.Address, "error", err.Error())
			errCount++
			continue
		}

		keepAmt := sdkmath.NewInt(entry.KeepPC).Mul(upcPerPC)

		spendable := bk.SpendableCoins(ctx, addr)
		spendableAmt := spendable.AmountOf(pchaintypes.BaseDenom)

		if spendableAmt.LTE(keepAmt) {
			ctx.Logger().Info("supply-burn: spendable <= keep amount, skipping",
				"address", entry.Address,
				"spendable_upc", spendableAmt.String(),
				"keep_upc", keepAmt.String())
			skipped++
			continue
		}

		burnAmt := spendableAmt.Sub(keepAmt)
		burnCoins := sdk.NewCoins(sdk.NewCoin(pchaintypes.BaseDenom, burnAmt))

		// Two-step burn: send to uexecutor module (has Burner permission), then burn.
		if err := bk.SendCoinsFromAccountToModule(ctx, addr, uexecutortypes.ModuleName, burnCoins); err != nil {
			ctx.Logger().Error("supply-burn: SendCoinsFromAccountToModule failed, skipping",
				"address", entry.Address, "error", err.Error())
			errCount++
			continue
		}

		if err := bk.BurnCoins(ctx, uexecutortypes.ModuleName, burnCoins); err != nil {
			// Coins left the account but BurnCoins failed: supply accounting is broken.
			panic("supply-burn: BurnCoins failed after SendCoinsFromAccountToModule: " + err.Error())
		}

		totalBurned = totalBurned.Add(burnAmt)
		success++
		ctx.Logger().Info("supply-burn: burned",
			"address", entry.Address,
			"burned_upc", burnAmt.String(),
			"kept_upc", keepAmt.String())
	}

	return totalBurned, success, skipped, errCount
}
