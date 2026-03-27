package aiauditfixes

import (
	"context"
	"fmt"

	"cosmossdk.io/log"
	storetypes "cosmossdk.io/store/types"
	upgradetypes "cosmossdk.io/x/upgrade/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"

	"github.com/pushchain/push-chain-node/app/upgrades"
	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
	utsstypes "github.com/pushchain/push-chain-node/x/utss/types"
)

const UpgradeName = "ai-audit-fixes"

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
		logger.Info("Starting upgrade handler")

		// ---------------------------------------------------------------
		// 1. Run module migrations FIRST (consensus version bumps)
		//    Must run before backfills so that the new proto schema
		//    (e.g. revert_error field on UniversalTx) is applied
		//    before we iterate existing entries.
		//    - uexecutor: 5 → 6 (PendingOutbounds collection)
		//    - utss: 1 → 2 (TssEvents collections)
		// ---------------------------------------------------------------
		versionMap, err := mm.RunMigrations(ctx, configurator, fromVM)
		if err != nil {
			return nil, fmt.Errorf("RunMigrations: %w", err)
		}

		// ---------------------------------------------------------------
		// 2. Backfill PendingOutbounds index from existing UniversalTx
		//    (safe now — migrations have run, proto schema is current)
		// ---------------------------------------------------------------
		if err := backfillPendingOutbounds(ctx, ak, logger); err != nil {
			return nil, fmt.Errorf("backfillPendingOutbounds: %w", err)
		}

		// ---------------------------------------------------------------
		// 3. Backfill TssEvents from ProcessHistory + TssKeyHistory
		// ---------------------------------------------------------------
		if err := backfillTssEvents(ctx, ak, logger); err != nil {
			return nil, fmt.Errorf("backfillTssEvents: %w", err)
		}

		// ---------------------------------------------------------------
		// 4. Register usigverifier V2 precompile in EVM params
		// ---------------------------------------------------------------
		if err := registerPrecompileV2(sdkCtx, ak, logger); err != nil {
			return nil, fmt.Errorf("registerPrecompileV2: %w", err)
		}

		logger.Info("Upgrade complete", "upgrade", UpgradeName)
		return versionMap, nil
	}
}

// backfillPendingOutbounds iterates all UniversalTx entries and creates a
// PendingOutboundEntry for every outbound that is still in PENDING status.
func backfillPendingOutbounds(ctx context.Context, ak *upgrades.AppKeepers, logger log.Logger) error {
	keeper := ak.UExecutorKeeper
	logger.Info("Backfilling PendingOutbounds index")

	count := 0
	iter, err := keeper.UniversalTx.Iterate(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to create UniversalTx iterator: %w", err)
	}
	defer iter.Close()

	for ; iter.Valid(); iter.Next() {
		kv, err := iter.KeyValue()
		if err != nil {
			return fmt.Errorf("failed to read UniversalTx entry: %w", err)
		}

		utxId := kv.Key
		utx := kv.Value

		for _, ob := range utx.OutboundTx {
			if ob == nil {
				continue
			}
			if ob.OutboundStatus == uexecutortypes.Status_PENDING {
				entry := uexecutortypes.PendingOutboundEntry{
					OutboundId:    ob.Id,
					UniversalTxId: utxId,
					CreatedAt:     0, // unknown historical height
				}
				if err := keeper.PendingOutbounds.Set(ctx, ob.Id, entry); err != nil {
					return fmt.Errorf("failed to set pending outbound %s: %w", ob.Id, err)
				}
				count++
			}
		}
	}

	logger.Info("PendingOutbounds backfill complete", "total_indexed", count)
	return nil
}

// backfillTssEvents creates TssEvent entries from ProcessHistory and TssKeyHistory.
// Skips if events already exist (re-run guard).
func backfillTssEvents(ctx context.Context, ak *upgrades.AppKeepers, logger log.Logger) error {
	utssKeeper := ak.UTssKeeper
	logger.Info("Backfilling TssEvents")

	// Guard: skip if events already exist
	hasEvents := false
	_ = utssKeeper.TssEvents.Walk(ctx, nil, func(id uint64, event utsstypes.TssEvent) (bool, error) {
		hasEvents = true
		return true, nil
	})
	if hasEvents {
		logger.Info("TssEvents already exist, skipping backfill")
		return nil
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
		return false, nil
	})
	if err != nil {
		return fmt.Errorf("failed to backfill process history events: %w", err)
	}

	// Backfill from TssKeyHistory
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
			Status:       utsstypes.TssEventStatus_TSS_EVENT_COMPLETED,
			ProcessId:    key.ProcessId,
			Participants: key.Participants,
			KeyId:        key.KeyId,
			TssPubkey:    key.TssPubkey,
			BlockHeight:  key.FinalizedBlockHeight,
		}

		if err := utssKeeper.TssEvents.Set(ctx, eventId, tssEvent); err != nil {
			return true, fmt.Errorf("failed to store tss key finalized event: %w", err)
		}

		if key.FinalizedBlockHeight >= latestFinalizedBlockHeight {
			latestFinalizedBlockHeight = key.FinalizedBlockHeight
			latestFinalizedEventId = eventId
			hasFinalized = true
		}
		return false, nil
	})
	if err != nil {
		return fmt.Errorf("failed to backfill tss key history events: %w", err)
	}

	// Mark the latest finalized key event as ACTIVE
	if hasFinalized {
		latestEvent, err := utssKeeper.TssEvents.Get(ctx, latestFinalizedEventId)
		if err != nil {
			return fmt.Errorf("failed to get latest finalized event: %w", err)
		}
		latestEvent.Status = utsstypes.TssEventStatus_TSS_EVENT_ACTIVE
		if err := utssKeeper.TssEvents.Set(ctx, latestFinalizedEventId, latestEvent); err != nil {
			return fmt.Errorf("failed to update latest finalized event status: %w", err)
		}
	}

	logger.Info("TssEvents backfill complete")
	return nil
}

// registerPrecompileV2 adds the usigverifier V2 address to EVM ActiveStaticPrecompiles
// so the EVM recognizes it as a callable precompile.
func registerPrecompileV2(sdkCtx sdk.Context, ak *upgrades.AppKeepers, logger log.Logger) error {
	const usigVerifierV2Addr = "0xEC00000000000000000000000000000000000001"

	evmParams := ak.EVMKeeper.GetParams(sdkCtx)

	// Check if already registered
	for _, addr := range evmParams.ActiveStaticPrecompiles {
		if addr == usigVerifierV2Addr {
			logger.Info("usigverifier V2 precompile already registered in EVM params")
			return nil
		}
	}

	evmParams.ActiveStaticPrecompiles = append(evmParams.ActiveStaticPrecompiles, usigVerifierV2Addr)

	if err := ak.EVMKeeper.SetParams(sdkCtx, evmParams); err != nil {
		return fmt.Errorf("failed to set EVM params with new precompile: %w", err)
	}

	logger.Info("Registered usigverifier V2 precompile in EVM params", "address", usigVerifierV2Addr)
	return nil
}
