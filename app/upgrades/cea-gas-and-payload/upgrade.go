package ceagasandpayload

import (
	"context"

	storetypes "cosmossdk.io/store/types"
	upgradetypes "cosmossdk.io/x/upgrade/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"

	"github.com/pushchain/push-chain-node/app/upgrades"
)

const UpgradeName = "cea-gas-and-payload"

// NewUpgrade constructs the upgrade definition
func NewUpgrade() upgrades.Upgrade {
	return upgrades.Upgrade{
		UpgradeName:          UpgradeName,
		CreateUpgradeHandler: CreateUpgradeHandler,
		StoreUpgrades: storetypes.StoreUpgrades{
			Added:   []string{},
			Deleted: []string{},
		},
	}
}

func CreateUpgradeHandler(
	mm upgrades.ModuleManager,
	configurator module.Configurator,
	ak *upgrades.AppKeepers,
) upgradetypes.UpgradeHandler {
	return func(ctx context.Context, plan upgradetypes.Plan, fromVM module.VersionMap) (module.VersionMap, error) {
		sdkCtx := sdk.UnwrapSDKContext(ctx)
		logger := sdkCtx.Logger().With("upgrade", UpgradeName)
		logger.Info("Starting upgrade handler")

		// ── Feature 1 ───────────────────────────────────────────────────────────
		// isCEA is now supported for GAS_AND_PAYLOAD inbound tx type (previously
		// only FUNDS and FUNDS_AND_PAYLOAD). When isCEA=true, the recipient is
		// used directly instead of resolving UEA from sender identity. Three-way
		// check applies: UEA → deposit+autoswap+payload, smart contract →
		// deposit+autoswap+executeUniversalTx, neither → FAILED (no revert).
		// No state migration required – purely behavioural.
		logger.Info("Feature: isCEA support added for GAS_AND_PAYLOAD inbound tx type")

		// ── Feature 2 ───────────────────────────────────────────────────────────
		// GAS_AND_PAYLOAD and FUNDS_AND_PAYLOAD inbound tx types now accept
		// amount=0. When amount is zero, deposit/autoswap is skipped but UEA
		// deployment and payload execution still proceed. This enables pure
		// payload execution without requiring any token transfer.
		// No state migration required – validation and execution logic updated.
		logger.Info("Feature: zero-amount inbounds for GAS_AND_PAYLOAD and FUNDS_AND_PAYLOAD (skip deposit, execute payload)")

		// ── State migration ──────────────────────────────────────────────────────
		// No module version bump needed – all changes are behavioural.
		versionMap, err := mm.RunMigrations(ctx, configurator, fromVM)
		if err != nil {
			logger.Error("RunMigrations failed", "error", err)
			return nil, err
		}

		logger.Info("Upgrade complete", "upgrade", UpgradeName)
		return versionMap, nil
	}
}
