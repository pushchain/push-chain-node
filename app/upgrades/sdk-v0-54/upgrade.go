package sdkv054

import (
	"context"
	"fmt"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	upgradetypes "github.com/cosmos/cosmos-sdk/x/upgrade/types"

	"github.com/cosmos/cosmos-sdk/types/module"

	"github.com/pushchain/push-chain-node/app/upgrades"
)

// UpgradeName is the on-chain name of the cosmos-sdk v0.54 / cosmos-evm v0.7.0
// upgrade. It MUST match the plan name used in the MsgSoftwareUpgrade proposal.
const UpgradeName = "sdk-v0.54"

// NewUpgrade constructs the upgrade definition.
//
// The chain moves from cosmos-sdk v0.53.7 / cometbft v0.38 / ibc-go v10 to
// cosmos-sdk v0.54.3 / cometbft v0.39.3 / ibc-go v11.2.0 / wasmd v0.70.3 /
// cosmos-evm v0.7.0.
//
// Two modules are unwired because they have no cosmos-sdk v0.54 compatible
// release, and their stores are deleted:
//
//   - tokenfactory (strangelove-ventures; its latest release targets sdk v0.50 and
//     its app package imports the pre-0.54 cosmos-sdk/x/group path)
//   - group        (cosmossdk.io/x/group only ships v0.2.0-rc.1, which targets the
//     abandoned sdk v0.52 line and needs core/appmodule/v2 APIs)
//
// Both were verified empty on donut before deletion: bank total supply carries no
// `factory/...` denoms, and no group exists (group id 1 is absent), so no group
// policy accounts hold funds. Deleting these stores strands nothing.
//
// packet-forward-middleware and rate-limiting are NOT dropped — both moved into
// ibc-go core and are retained at their new import paths.
func NewUpgrade() upgrades.Upgrade {
	return upgrades.Upgrade{
		UpgradeName:          UpgradeName,
		CreateUpgradeHandler: CreateUpgradeHandler,
		StoreUpgrades: storetypes.StoreUpgrades{
			Added: []string{},
			Deleted: []string{
				"tokenfactory",
				"group",
			},
		},
	}
}

// CreateUpgradeHandler drops the removed modules from the consensus version map
// so RunMigrations does not try to init-genesis or migrate them, then runs the
// remaining module migrations.
func CreateUpgradeHandler(
	mm upgrades.ModuleManager,
	configurator module.Configurator,
	ak *upgrades.AppKeepers,
) upgradetypes.UpgradeHandler {
	return func(ctx context.Context, plan upgradetypes.Plan, fromVM module.VersionMap) (module.VersionMap, error) {
		for _, name := range []string{"tokenfactory", "group"} {
			delete(fromVM, name)
		}

		vm, err := mm.RunMigrations(ctx, configurator, fromVM)
		if err != nil {
			return nil, fmt.Errorf("%s: run migrations: %w", UpgradeName, err)
		}
		return vm, nil
	}
}
