package evmv050

import (
	"context"
	"fmt"

	storetypes "cosmossdk.io/store/types"
	upgradetypes "cosmossdk.io/x/upgrade/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"

	"github.com/pushchain/push-chain-node/app/upgrades"
)

// Upgrade for the pushchain/evm dependency bump from v0.4.0 to v0.5.0.
//
// Key changes shipped in cosmos/evm v0.5.0:
//   - Chain/denom config moved from app options to state/genesis (PR #661):
//     InitEvmCoinInfo must be called during the upgrade to persist coin info on-chain.
//   - EVMKeeper.NewKeeper takes new evmChainID uint64 parameter.
//   - Precompile constructors changed signature (MsgServer/QueryServer injection).
//   - Ante decorators (EVMMonoDecorator, GasWantedDecorator) take pre-fetched Params.
//   - WithChainConfig removed from EVMConfigurator; SetChainConfig replaces it.
//   - cosmos/evm/types package removed; HasDynamicFeeExtensionOption moved to ante/types.
const UpgradeName = "evm-v0-5-0"

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
	keepers *upgrades.AppKeepers,
) upgradetypes.UpgradeHandler {
	return func(ctx context.Context, _ upgradetypes.Plan, fromVM module.VersionMap) (module.VersionMap, error) {
		sdkCtx := sdk.UnwrapSDKContext(ctx)
		logger := sdkCtx.Logger().With("upgrade", UpgradeName)
		logger.Info("Starting upgrade handler")
		logger.Info("pushchain/evm v0.4.0 → v0.5.0: coin info state migration, precompile/ante API updates")

		// InitEvmCoinInfo is required in v0.5 — chain denom/decimal config is now stored
		// on-chain rather than derived purely from app options at startup.
		if err := keepers.EVMKeeper.InitEvmCoinInfo(sdkCtx); err != nil {
			return nil, fmt.Errorf("InitEvmCoinInfo: %w", err)
		}

		versionMap, err := mm.RunMigrations(ctx, configurator, fromVM)
		if err != nil {
			return nil, fmt.Errorf("RunMigrations: %w", err)
		}

		logger.Info("Upgrade complete", "upgrade", UpgradeName)
		return versionMap, nil
	}
}
