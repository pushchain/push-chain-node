package inbound

import (
	"context"
	"fmt"

	upgradetypes "cosmossdk.io/x/upgrade/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"

	storetypes "cosmossdk.io/store/types"
	"github.com/pushchain/push-chain-node/app/upgrades"
	utsstypes "github.com/pushchain/push-chain-node/x/utss/types"
)

const UpgradeName = "tss-core-evm-params-fix"

// NewUpgrade constructs the upgrade definition
func NewUpgrade() upgrades.Upgrade {
	return upgrades.Upgrade{
		UpgradeName:          UpgradeName,
		CreateUpgradeHandler: CreateUpgradeHandler,
		StoreUpgrades: storetypes.StoreUpgrades{
			Added:   []string{utsstypes.StoreKey},
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
		sdkCtx.Logger().Info("=== Starting EVM params fix upgrade ===")

		// Get current corrupted params
		evmParams := ak.EVMKeeper.GetParams(sdkCtx)
		sdkCtx.Logger().Info("Current corrupted extra_eips", "value", evmParams.ExtraEIPs)

		// Fix ExtraEIPs - replace corrupted ASCII values with proper EIP number
		evmParams.ExtraEIPs = []int64{3855}

		if err := ak.EVMKeeper.SetParams(sdkCtx, evmParams); err != nil {
			return nil, fmt.Errorf("failed to set EVM params: %w", err)
		}
		// Run module migrations
		return mm.RunMigrations(ctx, configurator, fromVM)
	}
}
