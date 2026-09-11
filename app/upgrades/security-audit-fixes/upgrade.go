package securityauditfixes

import (
	"context"

	storetypes "cosmossdk.io/store/types"
	upgradetypes "cosmossdk.io/x/upgrade/types"
	"github.com/cosmos/cosmos-sdk/types/module"

	"github.com/pushchain/push-chain-node/app/upgrades"
)

// UpgradeName is the on-chain name for the 2026 security-audit fixes upgrade.
const UpgradeName = "security-audit-fixes"

func NewUpgrade() upgrades.Upgrade {
	return upgrades.Upgrade{
		UpgradeName:          UpgradeName,
		CreateUpgradeHandler: CreateUpgradeHandler,
		// No new module stores. ExpiredInbounds and the ballot-domain prefixes
		// are new prefixes inside the existing uexecutor store, not new KV stores.
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
		// RunMigrations executes the registered module migrations for the version
		// delta:
		//   - uexecutor v6 → v7: PendingInbounds KeySet → variant-aware Map
		//     reshape (F-2026-16642).
		//   - uregistry v3 → v4: canonical token storage keys + PRC20 reverse
		//     index backfill (F-2026-17022).
		// Every other audit fix in this upgrade is code-only (no state change).
		return mm.RunMigrations(ctx, configurator, fromVM)
	}
}
