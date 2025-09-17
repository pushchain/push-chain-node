package inbound

import (
	"context"

	storetypes "cosmossdk.io/store/types"
	upgradetypes "cosmossdk.io/x/upgrade/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"

	"github.com/pushchain/push-chain-node/app/upgrades"
	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
	uregistrytypes "github.com/pushchain/push-chain-node/x/uregistry/types"
	uvalidatortypes "github.com/pushchain/push-chain-node/x/uvalidator/types"
)

const UpgradeName = "inbound"

// NewUpgrade constructs the upgrade definition
func NewUpgrade() upgrades.Upgrade {
	return upgrades.Upgrade{
		UpgradeName:          UpgradeName,
		CreateUpgradeHandler: CreateUpgradeHandler,
		StoreUpgrades: storetypes.StoreUpgrades{
			Added: []string{
				uexecutortypes.InboundsName,    // new collection name
				uexecutortypes.UniversalTxName, // new collection name
				uregistrytypes.StoreKey,
				uvalidatortypes.StoreKey,
			},
			Deleted: []string{
				uexecutortypes.ChainConfigsName, // removed in this upgrade
			},
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
		sdkCtx.Logger().Info("🔧 Running upgrade:", "name", UpgradeName)

		// Initialize new modules
		genregistry := uregistrytypes.DefaultGenesis()
		if ak.URegistryKeeper != nil {
			if err := ak.URegistryKeeper.InitGenesis(sdkCtx, genregistry); err != nil {
				return nil, err
			}
		}

		sdkCtx.Logger().Info("Default genesis ran for uregistry")

		genvalidator := uvalidatortypes.DefaultGenesis()
		if ak.UValidatorKeeper != nil {
			if err := ak.UValidatorKeeper.InitGenesis(sdkCtx, genvalidator); err != nil {
				return nil, err
			}
		}

		sdkCtx.Logger().Info("Default genesis ran for uvalidator")

		sdkCtx.Logger().Info("✅ Upgrade handler done for URegistry, UValidator, UTxVerifier")
		sdkCtx.Logger().Info("Migrations will be run for UExecutor now")

		// Run module migrations
		return mm.RunMigrations(ctx, configurator, fromVM)
	}
}
