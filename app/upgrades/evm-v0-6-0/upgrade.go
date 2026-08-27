package evmv060

import (
	"context"
	"fmt"

	storetypes "cosmossdk.io/store/types"
	upgradetypes "cosmossdk.io/x/upgrade/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"

	"github.com/pushchain/push-chain-node/app/upgrades"
)

// UpgradeName is the on-chain name of the cosmos/evm v0.6.0 upgrade. It MUST
// match the `plan.name` used in the MsgSoftwareUpgrade governance proposal.
const UpgradeName = "evm-v0.6.0"

// NewUpgrade constructs the upgrade definition for the cosmos/evm v0.6.0 upgrade.
//
// Context: cosmos/evm v0.6.0 removed its bundled x/ibc/transfer wrapper, so
// push-chain now uses the standard ibc-go v10 transfer module (with the erc20
// IBC middleware wired in app.go). The "transfer" module therefore moves from
// ConsensusVersion 5 (the removed custom wrapper) to 6 (the standard module),
// which makes RunMigrations run the standard transfer v5->v6 migration
// (MigrateDenomTraceToDenom).
//
// No store keys are added or removed — the transfer module keeps the same store
// key (ibctransfertypes.StoreKey) — so StoreUpgrades is empty. None of the evm
// modules (vm, erc20, feemarket, precisebank) bumped their ConsensusVersion.
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

// CreateUpgradeHandler runs the standard module migrations. RunMigrations walks
// every module from the stored version map to its current ConsensusVersion, so
// the transfer v5->v6 migration (and any other pending module migration bundled
// in this release) is executed automatically.
func CreateUpgradeHandler(
	mm upgrades.ModuleManager,
	configurator module.Configurator,
	_ *upgrades.AppKeepers,
) upgradetypes.UpgradeHandler {
	return func(ctx context.Context, _ upgradetypes.Plan, fromVM module.VersionMap) (module.VersionMap, error) {
		logger := sdk.UnwrapSDKContext(ctx).Logger().With("upgrade", UpgradeName)
		logger.Info("starting cosmos/evm v0.6.0 upgrade: running module migrations (transfer v5->v6)")

		versionMap, err := mm.RunMigrations(ctx, configurator, fromVM)
		if err != nil {
			return nil, fmt.Errorf("run migrations: %w", err)
		}

		logger.Info("cosmos/evm v0.6.0 upgrade complete")
		return versionMap, nil
	}
}
