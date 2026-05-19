package evmv040

import (
	"context"
	"fmt"

	storetypes "cosmossdk.io/store/types"
	upgradetypes "cosmossdk.io/x/upgrade/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"

	"github.com/pushchain/push-chain-node/app/upgrades"
)

// Upgrade for the pushchain/evm dependency bump from v0.3.x to v0.4.0.
//
// Key changes shipped in cosmos/evm v0.4.0:
//   - Cosmos SDK v0.53.4 and CometBFT v0.38.18 in upstream (our fork stays on v0.50.x)
//   - Post-audit security fixes (batches 1–5) applied to EVM state machine and precompiles
//   - Enforce single EVM transaction per Cosmos transaction (#294)
//   - Evidence precompile removed (#305) — push-chain did not register it; no cleanup needed
//   - Ante logic moved from evmd to the evm package for library consumers (#443)
//   - Non–go-ethereum JSON-RPC methods removed (#456)
//   - Various bug fixes: revert reason format, address codec, estimate gas, blockHash RPCs,
//     p256 test flakiness, precompile initialization, nil pointer in gov precompile, etc.
//
// No module ConsensusVersion is bumped for this upgrade — v0.4.0 has no STATE BREAKING
// changes; RunMigrations is a no-op for all EVM-side modules (vm stays at v2, erc20 and
// feemarket both stay at v1). This handler exists as a coordination point so all validators
// flip to the new evm library at the same block height.
const UpgradeName = "evm-v0-4-0"

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
		logger.Info("pushchain/evm v0.3.x → v0.4.0: security audit patches, single-EVM-tx enforcement, evidence precompile removed upstream")

		versionMap, err := mm.RunMigrations(ctx, configurator, fromVM)
		if err != nil {
			return nil, fmt.Errorf("RunMigrations: %w", err)
		}

		logger.Info("Upgrade complete", "upgrade", UpgradeName)
		return versionMap, nil
	}
}
