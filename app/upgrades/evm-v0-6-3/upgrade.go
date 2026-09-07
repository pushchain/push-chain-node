package evmv063

import (
	"context"
	"fmt"

	storetypes "cosmossdk.io/store/types"
	upgradetypes "cosmossdk.io/x/upgrade/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	"github.com/pushchain/push-chain-node/app/upgrades"
)

const UpgradeName = "evm-v0.6.3"

// NewUpgrade registers the cosmos/evm v0.6.2 -> v0.6.3 bump plus the read-state
// fixes that landed alongside it (#366).
//
// No state migration: across v0.6.2..v0.6.3 upstream changed two files, both in
// x/vm/statedb — no .proto, no store key, no ConsensusVersion bump. The
// push-chain changes add no store and no proto either. StoreUpgrades is empty and
// RunMigrations is expected to be a no-op; the handler exists so the plan has a
// name for cosmovisor to switch the binary on.
//
// The consensus-affecting changes carried by this binary, for the record:
//
// cosmos/evm v0.6.3:
//   - statedb AddBalance now panics on overflow instead of wrapping silently
//     (the counterpart to the SubBalance underflow fix in v0.6.2).
//   - StateDB.Commit is now atomic. The precompile path folded its dirty set into
//     the root ctx while the precompile writes went to the cache, so the two could
//     diverge; both now land in the same context. The normal path stages through a
//     cache context, so a failure part-way leaves ctx untouched rather than
//     half-written.
//
// push-chain (#366):
//   - MsgVoteReadResult is gasless. This is the reason the upgrade must be
//     height-coordinated: an old binary still deducts the fee, so the two disagree
//     on state and the app hash diverges.
//   - x/ucallback passes explicit gas limits instead of nil, which made the EVM
//     estimate and land on gas that starved the callback it was funding.
//   - Reads requested inside a UEA payload are now ingested. They run through
//     DerivedEVMCall, which never fires the post-tx hook, so the request was
//     emitted and escrowed with nothing recording it.
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

// CreateUpgradeHandler runs the standard module migrations. None is expected to
// fire; RunMigrations is called anyway so the stored version map stays consistent
// and any migration bundled by a dependency still executes rather than being
// silently skipped.
func CreateUpgradeHandler(
	mm upgrades.ModuleManager,
	configurator module.Configurator,
	_ *upgrades.AppKeepers,
) upgradetypes.UpgradeHandler {
	return func(ctx context.Context, _ upgradetypes.Plan, fromVM module.VersionMap) (module.VersionMap, error) {
		logger := sdk.UnwrapSDKContext(ctx).Logger().With("upgrade", UpgradeName)
		logger.Info("starting cosmos/evm v0.6.3 upgrade: no state migration expected")

		versionMap, err := mm.RunMigrations(ctx, configurator, fromVM)
		if err != nil {
			return nil, fmt.Errorf("run migrations: %w", err)
		}

		logger.Info("cosmos/evm v0.6.3 upgrade complete")
		return versionMap, nil
	}
}
