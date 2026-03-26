package pendingoutboundsindex

import (
	"context"

	storetypes "cosmossdk.io/store/types"
	upgradetypes "cosmossdk.io/x/upgrade/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"

	"github.com/pushchain/push-chain-node/app/upgrades"
	"github.com/pushchain/push-chain-node/x/uexecutor/types"
)

const UpgradeName = "pending-outbounds-index"

// NewUpgrade constructs the upgrade definition
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
	ak *upgrades.AppKeepers,
) upgradetypes.UpgradeHandler {
	return func(ctx context.Context, plan upgradetypes.Plan, fromVM module.VersionMap) (module.VersionMap, error) {
		sdkCtx := sdk.UnwrapSDKContext(ctx)
		logger := sdkCtx.Logger().With("upgrade", UpgradeName)
		logger.Info("Starting upgrade handler: backfilling PendingOutbounds index")

		keeper := ak.UExecutorKeeper
		count := 0

		// Iterate all UniversalTx entries and backfill pending outbounds
		iter, err := keeper.UniversalTx.Iterate(ctx, nil)
		if err != nil {
			logger.Error("Failed to create UniversalTx iterator", "error", err)
			return nil, err
		}
		defer iter.Close()

		for ; iter.Valid(); iter.Next() {
			kv, err := iter.KeyValue()
			if err != nil {
				logger.Error("Failed to read UniversalTx entry", "error", err)
				return nil, err
			}

			utxId := kv.Key
			utx := kv.Value

			for _, ob := range utx.OutboundTx {
				if ob == nil {
					continue
				}
				if ob.OutboundStatus == types.Status_PENDING {
					entry := types.PendingOutboundEntry{
						OutboundId:    ob.Id,
						UniversalTxId: utxId,
						CreatedAt:     0, // unknown historical height
					}
					if err := keeper.PendingOutbounds.Set(ctx, ob.Id, entry); err != nil {
						logger.Error("Failed to set pending outbound", "outbound_id", ob.Id, "error", err)
						return nil, err
					}
					count++
				}
			}

			// Log progress every 1000 UTXs
			if count > 0 && count%1000 == 0 {
				logger.Info("Backfill progress", "pending_outbounds_indexed", count)
			}
		}

		logger.Info("PendingOutbounds backfill complete", "total_indexed", count)

		versionMap, err := mm.RunMigrations(ctx, configurator, fromVM)
		if err != nil {
			logger.Error("RunMigrations failed", "error", err)
			return nil, err
		}

		logger.Info("Upgrade complete", "upgrade", UpgradeName)
		return versionMap, nil
	}
}
