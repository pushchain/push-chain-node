package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/pushchain/push-chain-node/x/utss/types"
)

// VoteFundMigration handles a validator's vote on an observed fund migration tx.
func (k Keeper) VoteFundMigration(
	ctx context.Context,
	universalValidator sdk.ValAddress,
	migrationId uint64,
	txHash string,
	success bool,
) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Step 1: Fetch migration and verify it's pending
	migration, err := k.FundMigrations.Get(ctx, migrationId)
	if err != nil {
		return fmt.Errorf("fund migration %d not found: %w", migrationId, err)
	}
	if migration.Status != types.FundMigrationStatus_FUND_MIGRATION_STATUS_PENDING {
		return fmt.Errorf("fund migration %d is already finalized (status: %s)", migrationId, migration.Status.String())
	}

	k.Logger().Info("fund migration vote received",
		"migration_id", migrationId,
		"validator", universalValidator.String(),
		"tx_hash", txHash,
		"success", success,
	)

	// Step 2: Vote on ballot using CacheContext for atomicity
	tmpCtx, commit := sdkCtx.CacheContext()

	isFinalized, _, err := k.VoteOnFundMigrationBallot(tmpCtx, universalValidator, migrationId, txHash, success)
	if err != nil {
		return errors.Wrap(err, "failed to vote on fund migration ballot")
	}

	// Step 3: If not finalized yet, commit only the vote and return
	if !isFinalized {
		k.Logger().Debug("fund migration vote recorded, awaiting quorum",
			"migration_id", migrationId,
			"validator", universalValidator.String(),
		)
		commit()
		return nil
	}

	// Step 4: Ballot finalized — update migration state
	if success {
		migration.Status = types.FundMigrationStatus_FUND_MIGRATION_STATUS_COMPLETED
	} else {
		migration.Status = types.FundMigrationStatus_FUND_MIGRATION_STATUS_FAILED
	}
	migration.CompletedBlock = sdkCtx.BlockHeight()
	migration.TxHash = txHash

	if err := k.FundMigrations.Set(tmpCtx, migrationId, migration); err != nil {
		return errors.Wrap(err, "failed to update fund migration")
	}

	// Remove from pending index
	if err := k.PendingMigrations.Remove(tmpCtx, migrationId); err != nil {
		return errors.Wrap(err, "failed to remove pending migration index")
	}

	// Commit all changes atomically
	commit()

	// Step 5: Emit completion event
	event, err := types.NewFundMigrationCompletedEvent(types.FundMigrationCompletedEventData{
		MigrationID: migrationId,
		Chain:       migration.Chain,
		TxHash:      txHash,
		Success:     success,
		BlockHeight: sdkCtx.BlockHeight(),
	})
	if err != nil {
		return errors.Wrap(err, "failed to create migration completed event")
	}
	sdkCtx.EventManager().EmitEvent(event)

	k.Logger().Info("fund migration finalized",
		"migration_id", migrationId,
		"chain", migration.Chain,
		"success", success,
		"tx_hash", txHash,
	)

	return nil
}
