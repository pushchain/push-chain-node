package removeutxverifier

import (
	"context"
	"fmt"

	"cosmossdk.io/log"
	storetypes "cosmossdk.io/store/types"
	upgradetypes "cosmossdk.io/x/upgrade/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"

	"github.com/pushchain/push-chain-node/app/upgrades"
)

const UpgradeName = "remove-utxverifier"

func NewUpgrade() upgrades.Upgrade {
	return upgrades.Upgrade{
		UpgradeName:          UpgradeName,
		CreateUpgradeHandler: CreateUpgradeHandler,
		StoreUpgrades: storetypes.StoreUpgrades{
			Added:   []string{},
			Deleted: []string{"utxverifier"},
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
		logger := sdkCtx.Logger().With("upgrade", UpgradeName)
		logger.Info("Starting upgrade handler")

		// 1. Remove utxverifier module from version map so RunMigrations doesn't try to migrate it.
		//    The store is deleted via StoreUpgrades.Deleted above.
		delete(fromVM, "utxverifier")

		// 2. Run module migrations
		versionMap, err := mm.RunMigrations(ctx, configurator, fromVM)
		if err != nil {
			return nil, fmt.Errorf("RunMigrations: %w", err)
		}

		// 3. Deregister utxhashverifier precompile from EVM ActiveStaticPrecompiles
		if err := deregisterUtxHashVerifierPrecompile(sdkCtx, ak, logger); err != nil {
			return nil, fmt.Errorf("deregisterUtxHashVerifierPrecompile: %w", err)
		}

		logger.Info("Upgrade complete")
		return versionMap, nil
	}
}

// deregisterUtxHashVerifierPrecompile removes the utxhashverifier precompile address
// from EVM ActiveStaticPrecompiles so the EVM no longer routes calls to it.
func deregisterUtxHashVerifierPrecompile(sdkCtx sdk.Context, ak *upgrades.AppKeepers, logger log.Logger) error {
	const utxHashVerifierAddr = "0x00000000000000000000000000000000000000CB"

	evmParams := ak.EVMKeeper.GetParams(sdkCtx)

	filtered := make([]string, 0, len(evmParams.ActiveStaticPrecompiles))
	removed := false
	for _, addr := range evmParams.ActiveStaticPrecompiles {
		if addr == utxHashVerifierAddr {
			removed = true
			continue
		}
		filtered = append(filtered, addr)
	}

	if !removed {
		logger.Info("utxhashverifier precompile not found in EVM params, skipping")
		return nil
	}

	evmParams.ActiveStaticPrecompiles = filtered

	if err := ak.EVMKeeper.SetParams(sdkCtx, evmParams); err != nil {
		return fmt.Errorf("failed to set EVM params after removing precompile: %w", err)
	}

	logger.Info("Deregistered utxhashverifier precompile from EVM params", "address", utxHashVerifierAddr)
	return nil
}
