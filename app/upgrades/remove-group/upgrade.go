package removegroup

import (
	"context"
	"fmt"

	storetypes "cosmossdk.io/store/types"
	upgradetypes "cosmossdk.io/x/upgrade/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	"github.com/cosmos/cosmos-sdk/x/group"

	"github.com/pushchain/push-chain-node/app/upgrades"
)

const UpgradeName = "remove-group"

// NewUpgrade removes the x/group module from the chain.
//
// x/group is unused on Push (no group, no group policy and no proposal is ever
// created by the chain, the universal client or any user-facing flow) but it is
// a generic message dispatcher: MsgSubmitProposal/MsgExec unpack and route
// arbitrary nested sdk.Msgs straight to the message router, after the ante
// handler has already run. An MsgEthereumTx nested that way therefore skips the
// whole EVM ante chain - signature, nonce and gas checks (F-2026-18197).
//
// Removing the module deletes the dispatcher instead of trying to enumerate the
// message shapes it can carry. The store is dropped via StoreUpgrades.Deleted.
func NewUpgrade() upgrades.Upgrade {
	return upgrades.Upgrade{
		UpgradeName:          UpgradeName,
		CreateUpgradeHandler: CreateUpgradeHandler,
		StoreUpgrades: storetypes.StoreUpgrades{
			Added:   []string{},
			Deleted: []string{group.StoreKey},
		},
	}
}

func CreateUpgradeHandler(
	mm upgrades.ModuleManager,
	configurator module.Configurator,
	ak *upgrades.AppKeepers,
) upgradetypes.UpgradeHandler {
	return func(ctx context.Context, plan upgradetypes.Plan, fromVM module.VersionMap) (module.VersionMap, error) {
		logger := sdk.UnwrapSDKContext(ctx).Logger().With("upgrade", UpgradeName)
		logger.Info("Starting upgrade handler")

		// Drop x/group from the consensus version map so RunMigrations does not
		// try to migrate a module that is no longer registered. The KV store
		// itself is pruned by StoreUpgrades.Deleted above.
		delete(fromVM, group.ModuleName)

		versionMap, err := mm.RunMigrations(ctx, configurator, fromVM)
		if err != nil {
			return nil, fmt.Errorf("RunMigrations: %w", err)
		}

		logger.Info("Upgrade complete", "removed_module", group.ModuleName)
		return versionMap, nil
	}
}
