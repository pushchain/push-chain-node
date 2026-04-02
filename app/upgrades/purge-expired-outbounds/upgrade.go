package purgeexpiredoutbounds

import (
	"context"
	"fmt"

	storetypes "cosmossdk.io/store/types"
	upgradetypes "cosmossdk.io/x/upgrade/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	"github.com/pushchain/push-chain-node/app/upgrades"
	"github.com/pushchain/push-chain-node/x/uexecutor/types"
	uvalidatorkeeper "github.com/pushchain/push-chain-node/x/uvalidator/keeper"
	uvalidatortypes "github.com/pushchain/push-chain-node/x/uvalidator/types"
)

const UpgradeName = "purge-expired-outbounds"

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
		logger := sdkCtx.Logger()
		logger.Info("Running upgrade", "name", UpgradeName)

		deleted, skipped, errors := safeDeleteExpiredOutboundBallots(sdkCtx, ak)

		logger.Info("purge-expired-outbounds: upgrade complete",
			"ballots_deleted", deleted,
			"skipped", skipped,
			"errors", errors,
		)

		return mm.RunMigrations(ctx, configurator, fromVM)
	}
}

// safeDeleteExpiredOutboundBallots wraps deleteExpiredOutboundBallots with panic recovery
// so that any unexpected error (nil keeper, store corruption, etc.) does not halt the chain upgrade.
func safeDeleteExpiredOutboundBallots(ctx sdk.Context, ak *upgrades.AppKeepers) (deleted, skipped, errCount int) {
	defer func() {
		if r := recover(); r != nil {
			ctx.Logger().Error("purge-expired-outbounds: recovered from panic, upgrade will continue",
				"panic", fmt.Sprintf("%v", r),
				"deleted_before_panic", deleted,
			)
			errCount++
		}
	}()
	return deleteExpiredOutboundBallots(ctx, ak)
}

// deleteExpiredOutboundBallots iterates all pending outbounds, finds their expired
// ballots, and deletes them. This allows validators to re-vote on the outbounds,
// creating fresh ballots with the current validator set and large expiry.
func deleteExpiredOutboundBallots(ctx sdk.Context, ak *upgrades.AppKeepers) (deleted, skipped, errCount int) {
	logger := ctx.Logger()

	if ak == nil || ak.UExecutorKeeper == nil || ak.UValidatorKeeper == nil {
		logger.Error("purge-expired-outbounds: keeper is nil, skipping")
		return 0, 0, 1
	}

	ek := ak.UExecutorKeeper
	vk := ak.UValidatorKeeper

	// Phase 1: collect all pending outbound entries (avoid mutating during iteration)
	type pendingItem struct {
		outboundId    string
		universalTxId string
	}
	var items []pendingItem

	err := ek.PendingOutbounds.Walk(ctx, nil, func(key string, entry types.PendingOutboundEntry) (bool, error) {
		items = append(items, pendingItem{
			outboundId:    entry.OutboundId,
			universalTxId: entry.UniversalTxId,
		})
		return false, nil
	})
	if err != nil {
		logger.Error("purge-expired-outbounds: failed to walk pending outbounds", "error", err)
		return 0, 0, 1
	}

	logger.Info("purge-expired-outbounds: found pending outbounds", "count", len(items))

	// Phase 2: for each pending outbound, find and delete expired ballots
	for _, item := range items {
		if item.outboundId == "" || item.universalTxId == "" {
			logger.Error("purge-expired-outbounds: skipping entry with empty ID",
				"outbound_id", item.outboundId,
				"utx_id", item.universalTxId,
			)
			errCount++
			continue
		}

		ballotKey, found := findExpiredBallot(ctx, vk, item.universalTxId, item.outboundId)
		if !found {
			logger.Debug("purge-expired-outbounds: no expired ballot found, skipping",
				"outbound_id", item.outboundId,
			)
			skipped++
			continue
		}

		if err := vk.DeleteBallot(ctx, ballotKey); err != nil {
			logger.Error("purge-expired-outbounds: failed to delete ballot",
				"ballot_key", ballotKey,
				"outbound_id", item.outboundId,
				"error", err,
			)
			errCount++
			continue
		}

		deleted++
		logger.Info("purge-expired-outbounds: deleted expired ballot",
			"ballot_key", ballotKey,
			"outbound_id", item.outboundId,
			"utx_id", item.universalTxId,
		)
	}

	return deleted, skipped, errCount
}

// findExpiredBallot tries common observation variants to locate the expired ballot
// for a given outbound. Returns the ballot key and true if found, or ("", false).
func findExpiredBallot(ctx sdk.Context, vk *uvalidatorkeeper.Keeper, utxId, outboundId string) (string, bool) {
	observations := []types.OutboundObservation{
		{Success: false, ErrorMsg: "event expired before TSS could start", GasFeeUsed: "0"},
		{Success: false, ErrorMsg: "event expired before TSS could start"},
		{Success: false},
		{Success: true},
		{Success: false, GasFeeUsed: "0"},
	}

	for _, obs := range observations {
		ballotKey, err := types.GetOutboundBallotKey(utxId, outboundId, obs)
		if err != nil {
			continue
		}

		ballot, err := vk.Ballots.Get(ctx, ballotKey)
		if err != nil {
			continue
		}

		if ballot.Status == uvalidatortypes.BallotStatus_BALLOT_STATUS_EXPIRED {
			return ballotKey, true
		}
	}

	return "", false
}
