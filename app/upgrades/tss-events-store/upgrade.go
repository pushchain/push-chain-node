package tsseventsstore

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

const UpgradeName = "tss-events-store"

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
		sdkCtx.Logger().Info("Running upgrade:", "name", UpgradeName)

		utssKeeper := ak.UTssKeeper

		// Guard against re-run: if events already exist, skip backfill
		hasEvents := false
		_ = utssKeeper.TssEvents.Walk(ctx, nil, func(id uint64, event utsstypes.TssEvent) (bool, error) {
			hasEvents = true
			return true, nil // stop on first found
		})
		if hasEvents {
			sdkCtx.Logger().Info("TSS events already exist, skipping backfill")
			return mm.RunMigrations(ctx, configurator, fromVM)
		}

		// Backfill from ProcessHistory
		err := utssKeeper.ProcessHistory.Walk(ctx, nil, func(processId uint64, process utsstypes.TssKeyProcess) (bool, error) {
			eventId, err := utssKeeper.NextTssEventId.Next(ctx)
			if err != nil {
				return true, fmt.Errorf("failed to generate tss event id: %w", err)
			}

			var status utsstypes.TssEventStatus
			switch process.Status {
			case utsstypes.TssKeyProcessStatus_TSS_KEY_PROCESS_PENDING:
				status = utsstypes.TssEventStatus_TSS_EVENT_ACTIVE
			case utsstypes.TssKeyProcessStatus_TSS_KEY_PROCESS_SUCCESS:
				status = utsstypes.TssEventStatus_TSS_EVENT_COMPLETED
			case utsstypes.TssKeyProcessStatus_TSS_KEY_PROCESS_FAILED:
				status = utsstypes.TssEventStatus_TSS_EVENT_EXPIRED
			default:
				status = utsstypes.TssEventStatus_TSS_EVENT_ACTIVE
			}

			tssEvent := utsstypes.TssEvent{
				Id:           eventId,
				EventType:    utsstypes.TssEventType_TSS_EVENT_PROCESS_INITIATED,
				Status:       status,
				ProcessId:    process.Id,
				ProcessType:  process.ProcessType.String(),
				Participants: process.Participants,
				ExpiryHeight: process.ExpiryHeight,
				BlockHeight:  process.BlockHeight,
			}

			if err := utssKeeper.TssEvents.Set(ctx, eventId, tssEvent); err != nil {
				return true, fmt.Errorf("failed to store tss event: %w", err)
			}

			return false, nil // continue
		})
		if err != nil {
			return nil, fmt.Errorf("failed to backfill process history events: %w", err)
		}

		// Backfill from TssKeyHistory
		// Track the latest finalized key event ID to mark it as ACTIVE
		var latestFinalizedEventId uint64
		var latestFinalizedBlockHeight int64
		hasFinalized := false

		err = utssKeeper.TssKeyHistory.Walk(ctx, nil, func(keyId string, key utsstypes.TssKey) (bool, error) {
			eventId, err := utssKeeper.NextTssEventId.Next(ctx)
			if err != nil {
				return true, fmt.Errorf("failed to generate tss event id: %w", err)
			}

			tssEvent := utsstypes.TssEvent{
				Id:           eventId,
				EventType:    utsstypes.TssEventType_TSS_EVENT_KEY_FINALIZED,
				Status:       utsstypes.TssEventStatus_TSS_EVENT_COMPLETED, // default to completed
				ProcessId:    key.ProcessId,
				ProcessType:  "", // not stored in TssKey
				Participants: key.Participants,
				KeyId:        key.KeyId,
				TssPubkey:    key.TssPubkey,
				BlockHeight:  key.FinalizedBlockHeight,
			}

			if err := utssKeeper.TssEvents.Set(ctx, eventId, tssEvent); err != nil {
				return true, fmt.Errorf("failed to store tss key finalized event: %w", err)
			}

			// Track the latest finalized key by block height
			if key.FinalizedBlockHeight >= latestFinalizedBlockHeight {
				latestFinalizedBlockHeight = key.FinalizedBlockHeight
				latestFinalizedEventId = eventId
				hasFinalized = true
			}

			return false, nil // continue
		})
		if err != nil {
			return nil, fmt.Errorf("failed to backfill tss key history events: %w", err)
		}

		// Mark the latest finalized key event as ACTIVE
		if hasFinalized {
			latestEvent, err := utssKeeper.TssEvents.Get(ctx, latestFinalizedEventId)
			if err != nil {
				return nil, fmt.Errorf("failed to get latest finalized event: %w", err)
			}
			latestEvent.Status = utsstypes.TssEventStatus_TSS_EVENT_ACTIVE
			if err := utssKeeper.TssEvents.Set(ctx, latestFinalizedEventId, latestEvent); err != nil {
				return nil, fmt.Errorf("failed to update latest finalized event status: %w", err)
			}
		}

		sdkCtx.Logger().Info("TSS events backfill completed")

		// Run module migrations
		return mm.RunMigrations(ctx, configurator, fromVM)
	}
}
