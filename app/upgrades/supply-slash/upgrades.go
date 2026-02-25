package supplyslash

import (
	"context"
	_ "embed"
	"encoding/json"

	sdkmath "cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"
	upgradetypes "cosmossdk.io/x/upgrade/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"

	"github.com/pushchain/push-chain-node/app/upgrades"
	pchaintypes "github.com/pushchain/push-chain-node/types"
	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
	uvalidatortypes "github.com/pushchain/push-chain-node/x/uvalidator/types"
)

const UpgradeName = "supply-slash"

// SlashEntry represents one address/amount pair from rebalance.json.
// AmountUPC and RebalancedTokens are in whole PC tokens (already divided by 10^18).
// Burn amount per address = (AmountUPC - RebalancedTokens) * 10^18 upc.
type SlashEntry struct {
	Address          string  `json:"address"`
	AmountUPC        float64 `json:"amount_upc"`        // current balance in whole PC
	RebalancedTokens float64 `json:"rebalanced_tokens"` // target balance in whole PC
}

//go:embed rebalance.json
var slashListJSON []byte

// NewUpgrade constructs the upgrade definition.
func NewUpgrade() upgrades.Upgrade {
	return upgrades.Upgrade{
		UpgradeName:          UpgradeName,
		CreateUpgradeHandler: CreateUpgradeHandler,
		StoreUpgrades:        storetypes.StoreUpgrades{},
	}
}

// CreateUpgradeHandler runs two operations in order:
//  1. Flush the entire uvalidator module balance to the distribution module,
//     clearing coins that accumulated due to a prior BeginBlocker bug.
//  2. Burn (AmountUPC - RebalancedTokens) * 10^18 upc from each address's
//     liquid balance as specified in the embedded rebalance.json,
//     reducing total supply to ~10B PC.
func CreateUpgradeHandler(
	mm upgrades.ModuleManager,
	configurator module.Configurator,
	ak *upgrades.AppKeepers,
) upgradetypes.UpgradeHandler {
	return func(ctx context.Context, plan upgradetypes.Plan, fromVM module.VersionMap) (module.VersionMap, error) {
		sdkCtx := sdk.UnwrapSDKContext(ctx)
		sdkCtx.Logger().Info("Running upgrade", "name", UpgradeName)

		// Operation 1: flush accumulated uvalidator coins to distribution module
		if err := FlushUValidatorToDistribution(sdkCtx, ak.BankKeeper); err != nil {
			panic("supply-slash: uvalidator flush failed: " + err.Error())
		}

		// Operation 2: parse embedded JSON and slash addresses
		var entries []SlashEntry
		if err := json.Unmarshal(slashListJSON, &entries); err != nil {
			panic("supply-slash: failed to parse rebalance.json: " + err.Error())
		}

		totalBurned, success, skipped, errCount := ExecuteSlashEntries(sdkCtx, ak.BankKeeper, entries)
		sdkCtx.Logger().Info("supply-slash: upgrade complete",
			"total_burned_upc", totalBurned.String(),
			"success", success,
			"skipped", skipped,
			"errors", errCount,
		)

		return mm.RunMigrations(ctx, configurator, fromVM)
	}
}

// FlushUValidatorToDistribution sends the entire uvalidator module balance to the
// distribution module. This clears coins that accumulated due to a prior BeginBlocker
// bug where extraCoins were retained in the uvalidator account instead of being
// forwarded to distribution.
//
// Note: uvalidator has nil permissions in maccPerms, but SendCoinsFromModuleToModule
// does not require Burner permission — it performs a regular SendCoins between two
// module addresses.
func FlushUValidatorToDistribution(ctx sdk.Context, bk bankkeeper.BaseKeeper) error {
	uvalidatorAddr := authtypes.NewModuleAddress(uvalidatortypes.ModuleName)
	balance := bk.GetAllBalances(ctx, uvalidatorAddr)
	if balance.IsZero() {
		ctx.Logger().Info("supply-slash: uvalidator balance is zero, nothing to flush")
		return nil
	}

	if err := bk.SendCoinsFromModuleToModule(ctx, uvalidatortypes.ModuleName, distrtypes.ModuleName, balance); err != nil {
		return err
	}

	ctx.Logger().Info("supply-slash: flushed uvalidator to distribution", "amount", balance.String())
	return nil
}

// ExecuteSlashEntries burns (entry.AmountUPC - entry.RebalancedTokens) * 10^18 upc
// of liquid balance from each address. Returns total burned and per-outcome counts.
//
// Entries are skipped (not counted as errors) when:
//   - burn amount <= 0 (address already at or below target)
//   - spendable balance < burn amount
//
// Entries are counted as errors when:
//   - address fails bech32 decoding
//
// Panics if BurnCoins fails after SendCoinsFromAccountToModule succeeds, since that
// would leave supply accounting in an inconsistent state.
func ExecuteSlashEntries(
	ctx sdk.Context,
	bk bankkeeper.BaseKeeper,
	entries []SlashEntry,
) (totalBurned sdkmath.Int, success, skipped, errCount int) {
	totalBurned = sdkmath.ZeroInt()

	for _, entry := range entries {
		addr, err := sdk.AccAddressFromBech32(entry.Address)
		if err != nil {
			ctx.Logger().Error("supply-slash: invalid address, skipping",
				"address", entry.Address, "error", err.Error())
			errCount++
			continue
		}

		// Compute burn in whole PC, then scale to upc (×10^18).
		// float64 is safe here: all values are whole numbers ending in .0,
		// and the largest value (~8.2×10^12) is well within float64's exact
		// integer range (2^53 ≈ 9×10^15).
		burnPC := int64(entry.AmountUPC) - int64(entry.RebalancedTokens)
		if burnPC <= 0 {
			ctx.Logger().Info("supply-slash: burn amount <= 0, skipping",
				"address", entry.Address,
				"amount_upc", entry.AmountUPC,
				"rebalanced_tokens", entry.RebalancedTokens)
			skipped++
			continue
		}

		burnAmt := sdkmath.NewInt(burnPC).Mul(sdkmath.NewInt(1_000_000_000_000_000_000))
		burnCoins := sdk.NewCoins(sdk.NewCoin(pchaintypes.BaseDenom, burnAmt))

		// Only touch liquid (spendable) balance — never delegated/staked tokens.
		spendable := bk.SpendableCoins(ctx, addr)
		if spendable.AmountOf(pchaintypes.BaseDenom).LT(burnAmt) {
			ctx.Logger().Info("supply-slash: insufficient spendable balance, skipping",
				"address", entry.Address,
				"available_upc", spendable.AmountOf(pchaintypes.BaseDenom).String(),
				"required_upc", burnAmt.String())
			skipped++
			continue
		}

		// Two-step burn: send to uexecutor module (has Burner permission), then burn.
		if err := bk.SendCoinsFromAccountToModule(ctx, addr, uexecutortypes.ModuleName, burnCoins); err != nil {
			ctx.Logger().Error("supply-slash: SendCoinsFromAccountToModule failed, skipping",
				"address", entry.Address, "error", err.Error())
			errCount++
			continue
		}

		if err := bk.BurnCoins(ctx, uexecutortypes.ModuleName, burnCoins); err != nil {
			// Coins left the account but BurnCoins failed: supply accounting is broken.
			panic("supply-slash: BurnCoins failed after SendCoinsFromAccountToModule: " + err.Error())
		}

		totalBurned = totalBurned.Add(burnAmt)
		success++
		ctx.Logger().Info("supply-slash: burned",
			"address", entry.Address, "amount_upc", burnAmt.String())
	}

	return totalBurned, success, skipped, errCount
}
