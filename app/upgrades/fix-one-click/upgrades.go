package oneclickexec

import (
	"context"

	storetypes "cosmossdk.io/store/types"
	upgradetypes "cosmossdk.io/x/upgrade/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"

	"github.com/rollchains/pchain/app/upgrades"
	ocvprecompile "github.com/rollchains/pchain/precompiles/ocv"
)

const UpgradeName = "fix-one-click"

// NewUpgrade constructs the upgrade definition
func NewUpgrade() upgrades.Upgrade {
	return upgrades.Upgrade{
		UpgradeName:          UpgradeName,
		CreateUpgradeHandler: CreateUpgradeHandler,
		StoreUpgrades: storetypes.StoreUpgrades{
			Added:   []string{}, // No new store keys
			Deleted: []string{}, // Optionally delete old keys
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

		evmParams := ak.EVMKeeper.GetParams(sdkCtx)

		// Check if OCV precompile is already in the active list
		ocvExists := false

		for _, addr := range evmParams.ActiveStaticPrecompiles {
			if addr == ocvprecompile.OcvPrecompileAddress {
				ocvExists = true
				break
			}
		}

		// Add OCV precompile if not already present
		if !ocvExists {
			evmParams.ActiveStaticPrecompiles = append(evmParams.ActiveStaticPrecompiles, ocvprecompile.OcvPrecompileAddress)
			ak.EVMKeeper.SetParams(sdkCtx, evmParams)
		}

		// Run remaining module migrations
		return mm.RunMigrations(ctx, configurator, fromVM)
	}
}
