package sdkv053

import (
	"context"
	"fmt"

	storetypes "cosmossdk.io/store/types"
	upgradetypes "cosmossdk.io/x/upgrade/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"

	"github.com/pushchain/push-chain-node/app/upgrades"
)

// UpgradeName is the on-chain name of the Cosmos SDK v0.53 upgrade. It MUST
// match the `plan.name` used in the MsgSoftwareUpgrade governance proposal.
const UpgradeName = "sdk-v0.53"

// NewUpgrade constructs the upgrade definition for the Cosmos SDK
// v0.50.10 -> v0.53.7 bump.
//
// This is a NO-OP state upgrade. v0.53 is an additive release over v0.50: none
// of the modules this chain registers bumped its ConsensusVersion (auth, bank,
// staking, gov, distribution, slashing, mint, consensus, authz, evidence,
// feegrant, circuit, group, nft — all unchanged 0.50->0.53), and no new store
// keys are added. v0.53's new modules (x/epochs, x/protocolpool) are not adopted,
// and the new unordered-transaction nonces live under a key prefix inside the
// existing x/auth store (unordered txs are left disabled). So StoreUpgrades is
// empty and RunMigrations is a no-op; the handler only swaps the chain to the
// v0.53 binary and resumes block production.
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

// CreateUpgradeHandler runs the standard module migrations. Because no module
// ConsensusVersion changed across the SDK v0.50 -> v0.53 bump, RunMigrations
// walks the version map and finds nothing to migrate (a no-op), then returns the
// current version map so the chain resumes on the new binary.
func CreateUpgradeHandler(
	mm upgrades.ModuleManager,
	configurator module.Configurator,
	_ *upgrades.AppKeepers,
) upgradetypes.UpgradeHandler {
	return func(ctx context.Context, _ upgradetypes.Plan, fromVM module.VersionMap) (module.VersionMap, error) {
		logger := sdk.UnwrapSDKContext(ctx).Logger().With("upgrade", UpgradeName)
		logger.Info("starting Cosmos SDK v0.53 upgrade (no-op: binary swap only; no module ConsensusVersion changes)")

		versionMap, err := mm.RunMigrations(ctx, configurator, fromVM)
		if err != nil {
			return nil, fmt.Errorf("run migrations: %w", err)
		}

		logger.Info("Cosmos SDK v0.53 upgrade complete")
		return versionMap, nil
	}
}
