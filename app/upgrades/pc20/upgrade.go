package pc20

import (
	"context"
	"fmt"

	storetypes "cosmossdk.io/store/types"
	upgradetypes "cosmossdk.io/x/upgrade/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"

	"github.com/pushchain/push-chain-node/app/upgrades"
)

// UpgradeName is the on-chain name of the pc20 upgrade. It MUST match the
// plan.name in the MsgSoftwareUpgrade governance proposal.
const UpgradeName = "pc20"

// NewUpgrade constructs the pc20 upgrade.
//
// This is a NO-OP state upgrade: the pc20 feature adds no new store keys, no
// module ConsensusVersion bumps, and no migrations — it is a binary-only change
// (new x/uexecutor pc20 handling + the VAULT_PC / VAULT_PC20 system-contract
// definitions in x/uregistry). The VAULT_PC (0xB0) and VAULT_PC20 (0xB1)
// contracts are deployed separately from the EVM side after the upgrade, so the
// handler does not touch EVM state. It only runs RunMigrations (a no-op here) so
// the chain swaps to the new binary and resumes producing blocks.
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
	_ *upgrades.AppKeepers,
) upgradetypes.UpgradeHandler {
	return func(ctx context.Context, _ upgradetypes.Plan, fromVM module.VersionMap) (module.VersionMap, error) {
		logger := sdk.UnwrapSDKContext(ctx).Logger().With("upgrade", UpgradeName)
		logger.Info("starting pc20 upgrade (no-op: binary swap only; VAULT_PC/VAULT_PC20 deployed separately from EVM side)")

		versionMap, err := mm.RunMigrations(ctx, configurator, fromVM)
		if err != nil {
			return nil, fmt.Errorf("run migrations: %w", err)
		}

		logger.Info("pc20 upgrade complete")
		return versionMap, nil
	}
}
