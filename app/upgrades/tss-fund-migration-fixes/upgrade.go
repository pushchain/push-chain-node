package tssfundmigrationfixes

import (
	"context"

	storetypes "cosmossdk.io/store/types"
	upgradetypes "cosmossdk.io/x/upgrade/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"

	"github.com/pushchain/push-chain-node/app/upgrades"
)

const UpgradeName = "tss-fund-migration-fixes"

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

// CreateUpgradeHandler runs the utss v3 → v4 migration which backfills
// FundMigration.l1_gas_fee on records stored before the field existed.
// The new gas_limit and l1_gas_fee values used by InitiateFundMigration are
// sourced from UniversalCore's tssFundMigrationGasLimitByChainNamespace and
// l1GasFeeByChainNamespace mappings at call time — no state seeding required.
func CreateUpgradeHandler(
	mm upgrades.ModuleManager,
	configurator module.Configurator,
	ak *upgrades.AppKeepers,
) upgradetypes.UpgradeHandler {
	return func(ctx context.Context, plan upgradetypes.Plan, fromVM module.VersionMap) (module.VersionMap, error) {
		sdkCtx := sdk.UnwrapSDKContext(ctx)
		logger := sdkCtx.Logger().With("upgrade", UpgradeName)
		logger.Info("Starting upgrade handler")
		logger.Info("Feature: FundMigration.gas_limit and l1_gas_fee now sourced from UniversalCore per-chain mappings")

		versionMap, err := mm.RunMigrations(ctx, configurator, fromVM)
		if err != nil {
			logger.Error("RunMigrations failed", "error", err)
			return nil, err
		}

		logger.Info("Upgrade complete", "upgrade", UpgradeName)
		return versionMap, nil
	}
}
