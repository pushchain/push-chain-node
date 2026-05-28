// Package evmparamsmigration contains the upgrade handler that migrates the
// x/vm module's on-chain Params from the v0.2.x proto layout to the v0.3.x
// layout.
//
// The schema change (ChainConfig removed from field 5, remaining fields
// shifted down) was not accompanied by a ConsensusVersion bump in the
// previous release, so the stored bytes are still in the old format.
// This upgrade bumps the vm module to ConsensusVersion 2 and runs the
// migration via RunMigrations.
package evmparamsmigration

import (
	"context"
	"fmt"

	storetypes "cosmossdk.io/store/types"
	upgradetypes "cosmossdk.io/x/upgrade/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"

	"github.com/pushchain/push-chain-node/app/upgrades"
)

const UpgradeName = "evm-params-migration032"

func NewUpgrade() upgrades.Upgrade {
	return upgrades.Upgrade{
		UpgradeName:          UpgradeName,
		CreateUpgradeHandler: CreateUpgradeHandler,
		StoreUpgrades:        storetypes.StoreUpgrades{},
	}
}

func CreateUpgradeHandler(
	mm upgrades.ModuleManager,
	configurator module.Configurator,
	_ *upgrades.AppKeepers,
) upgradetypes.UpgradeHandler {
	return func(ctx context.Context, _ upgradetypes.Plan, fromVM module.VersionMap) (module.VersionMap, error) {
		sdkCtx := sdk.UnwrapSDKContext(ctx)
		logger := sdkCtx.Logger().With("upgrade", UpgradeName)
		logger.Info("Migrating x/vm Params store from proto v0.2.x layout to v0.3.x layout")

		// RunMigrations detects that x/vm jumped from ConsensusVersion 1 → 2
		// and automatically calls Migrator.Migrate1to2, which rewrites the
		// Params KV entry with the corrected field numbering.
		versionMap, err := mm.RunMigrations(ctx, configurator, fromVM)
		if err != nil {
			return nil, fmt.Errorf("RunMigrations: %w", err)
		}

		logger.Info("x/vm Params migration complete")
		return versionMap, nil
	}
}
