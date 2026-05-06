package evmupgrade032

import (
	"context"

	storetypes "cosmossdk.io/store/types"
	upgradetypes "cosmossdk.io/x/upgrade/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"

	"github.com/pushchain/push-chain-node/app/upgrades"
)

const UpgradeName = "evm-upgrade032"

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

// CreateUpgradeHandler runs the cosmos/evm v0.3.2 upgrade migrations.
//
// Key changes in this library bump:
//   - AccountKeeper now requires unordered-tx methods (satisfied via authKeeperEVMWrapper stub)
//   - EVMKeeper constructor takes additional params: store keys map, ConsensusParamsKeeper
//   - go-ethereum bumped to v1.15.11-cosmos-0; statedb.Account.Balance is now uint256.Int (not big.Int)
//   - erc20 module: NativePrecompiles moved from Params to a top-level genesis field
//   - Precompile constructors now require bankKeeper; gov precompile also requires appCodec
//   - vm.NewAppModule signature updated to accept AddressCodec
//
// RunMigrations handles the erc20 state migration automatically (consensus version bump).
func CreateUpgradeHandler(
	mm upgrades.ModuleManager,
	configurator module.Configurator,
	ak *upgrades.AppKeepers,
) upgradetypes.UpgradeHandler {
	return func(ctx context.Context, plan upgradetypes.Plan, fromVM module.VersionMap) (module.VersionMap, error) {
		sdkCtx := sdk.UnwrapSDKContext(ctx)
		logger := sdkCtx.Logger().With("upgrade", UpgradeName)
		logger.Info("Starting upgrade handler")
		logger.Info("cosmos/evm bumped to v0.3.2: erc20 NativePrecompiles field migration, go-ethereum v1.15.11-cosmos-0")

		versionMap, err := mm.RunMigrations(ctx, configurator, fromVM)
		if err != nil {
			logger.Error("RunMigrations failed", "error", err)
			return nil, err
		}

		logger.Info("Upgrade complete", "upgrade", UpgradeName)
		return versionMap, nil
	}
}
