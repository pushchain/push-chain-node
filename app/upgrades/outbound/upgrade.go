package outbound

import (
	"context"

	storetypes "cosmossdk.io/store/types"
	upgradetypes "cosmossdk.io/x/upgrade/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"github.com/pushchain/push-chain-node/app/upgrades"
	uvalidatortypes "github.com/pushchain/push-chain-node/x/uvalidator/types"
)

const UpgradeName = "outbound"

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
		sdkCtx.Logger().Info("🔧 Running upgrade:", "name", UpgradeName)

		// --- ensure uvalidator module account exists ---
		if acc := ak.AccountKeeper.GetModuleAccount(sdkCtx, uvalidatortypes.ModuleName); acc == nil {
			ak.AccountKeeper.SetModuleAccount(
				sdkCtx,
				authtypes.NewEmptyModuleAccount(
					uvalidatortypes.ModuleName,
				),
			)

			sdkCtx.Logger().Info(
				"created missing module account",
				"module", uvalidatortypes.ModuleName,
			)
		}

		// Run module migrations
		return mm.RunMigrations(ctx, configurator, fromVM)
	}
}
