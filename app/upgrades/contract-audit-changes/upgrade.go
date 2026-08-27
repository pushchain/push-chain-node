package contractauditchanges

import (
	"context"
	"fmt"

	storetypes "cosmossdk.io/store/types"
	upgradetypes "cosmossdk.io/x/upgrade/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"

	"github.com/pushchain/push-chain-node/app/upgrades"
)

// Upgrade for chain contract audit changes + evm version bump to 0.3.2
// UpgradeName matches the chain-side change set that adapts the chain's
// UniversalCore ABI + gas-fee read path to the post-audit smart-contract
// No module ConsensusVersion is bumped for this upgrade — none of the chain
// changes touch module storage schemas, so RunMigrations is a no-op for the
// version map; this handler exists primarily as a coordination point so all
// validators flip to the new ABI / gas-fee read at the same height.

// Key changes in this library bump:
//   - AccountKeeper now requires unordered-tx methods (satisfied via authKeeperEVMWrapper stub)
//   - EVMKeeper constructor takes additional params: store keys map, ConsensusParamsKeeper
//   - go-ethereum bumped to v1.15.11-cosmos-0; statedb.Account.Balance is now uint256.Int (not big.Int)
//   - erc20 module: NativePrecompiles moved from Params to a top-level genesis field
//   - Precompile constructors now require bankKeeper; gov precompile also requires appCodec
//   - vm.NewAppModule signature updated to accept AddressCodec
//
// RunMigrations handles the erc20 state migration automatically (consensus version bump).
const UpgradeName = "changes"

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
		sdkCtx := sdk.UnwrapSDKContext(ctx)
		logger := sdkCtx.Logger().With("upgrade", UpgradeName)
		logger.Info("Starting upgrade handler")
		logger.Info("cosmos/evm v0.2.x → v0.3.2: erc20 NativePrecompiles field migration, go-ethereum v1.15.11-cosmos-0")
		logger.Info("UniversalCore audit: ABI gasLimitUsed output, setGasPrice removal, gas_fee.go reads results[5]")

		// erc20 module's ConsensusVersion bump (from EVM 0.3.2) drives the
		// only on-chain state migration this upgrade requires; RunMigrations
		// handles it. The chain's own modules don't bump versions here.
		versionMap, err := mm.RunMigrations(ctx, configurator, fromVM)
		if err != nil {
			return nil, fmt.Errorf("RunMigrations: %w", err)
		}

		logger.Info("Upgrade complete", "upgrade", UpgradeName)
		return versionMap, nil
	}
}
