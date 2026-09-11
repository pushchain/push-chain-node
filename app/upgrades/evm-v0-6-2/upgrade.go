package evmv062

import (
	"context"
	"fmt"

	storetypes "cosmossdk.io/store/types"
	upgradetypes "cosmossdk.io/x/upgrade/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	"github.com/pushchain/push-chain-node/app/upgrades"
)

const UpgradeName = "evm-v0.6.2"

// NewUpgrade registers the cosmos/evm v0.6.0 -> v0.6.2 upgrade.
//
// Unlike the v0.6.0 upgrade, this release carries NO state migration: across
// v0.6.0..v0.6.2 upstream changed no .proto file, added or removed no store key,
// and bumped no module's ConsensusVersion. StoreUpgrades is therefore empty and
// RunMigrations is expected to be a no-op — the handler exists so the plan has a
// name for cosmovisor to switch the binary on.
//
// The behavioural changes that DO land with this binary, for the record:
//   - statedb SubBalance now panics on balance underflow instead of wrapping
//     silently (upstream's equivalent of F-2026-18201).
//   - Module accounts may no longer have their EVM balance written
//     ("<addr> is not allowed to receive funds"). Push is unaffected: every
//     module-sender EVM call passes value=0, and the fee/burn and universal
//     validator reward paths run through the SDK bank keeper, which never
//     reaches the EVM statedb.
//   - Precompile out-of-gas now propagates as vm.ErrOutOfGas. No repricing:
//     succeeding transactions consume identical gas.
//   - Extended-denom accounting and the erc20 IBC v2 ack changes are no-ops on
//     this chain (ExtendedDenom == Denom == upc; no IBC v2 stack is wired).
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

// CreateUpgradeHandler runs the standard module migrations. No module migration
// is expected to fire for this release; RunMigrations is called anyway so the
// stored version map stays consistent and any migration bundled by a dependency
// is still executed rather than silently skipped.
func CreateUpgradeHandler(
	mm upgrades.ModuleManager,
	configurator module.Configurator,
	_ *upgrades.AppKeepers,
) upgradetypes.UpgradeHandler {
	return func(ctx context.Context, _ upgradetypes.Plan, fromVM module.VersionMap) (module.VersionMap, error) {
		logger := sdk.UnwrapSDKContext(ctx).Logger().With("upgrade", UpgradeName)
		logger.Info("starting cosmos/evm v0.6.2 upgrade: no state migration expected")

		versionMap, err := mm.RunMigrations(ctx, configurator, fromVM)
		if err != nil {
			return nil, fmt.Errorf("run migrations: %w", err)
		}

		logger.Info("cosmos/evm v0.6.2 upgrade complete")
		return versionMap, nil
	}
}
