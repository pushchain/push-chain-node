package keeper

import (
	"context"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/pushchain/push-chain-node/x/utss/types"
)

const nativeTransferGasLimit = 21000

// InitiateFundMigration validates and creates a fund migration from an old TSS key vault
// to the current TSS key vault for a specific chain.
func (k Keeper) InitiateFundMigration(ctx context.Context, oldKeyId, chain string) (uint64, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// 1. Validate old key exists in history
	oldKey, err := k.TssKeyHistory.Get(ctx, oldKeyId)
	if err != nil {
		return 0, fmt.Errorf("old key %s not found in TssKeyHistory: %w", oldKeyId, err)
	}

	// 2. Verify old key was produced by keygen (not refresh or quorum change)
	process, err := k.ProcessHistory.Get(ctx, oldKey.ProcessId)
	if err != nil {
		return 0, fmt.Errorf("process %d for key %s not found: %w", oldKey.ProcessId, oldKeyId, err)
	}
	if process.ProcessType != types.TssProcessType_TSS_PROCESS_KEYGEN {
		return 0, fmt.Errorf("key %s was produced by %s, not keygen; migration only needed after keygen",
			oldKeyId, process.ProcessType.String())
	}

	// 3. Verify old key != current key
	currentKey, err := k.CurrentTssKey.Get(ctx)
	if err != nil {
		return 0, fmt.Errorf("no current TSS key set: %w", err)
	}
	if oldKeyId == currentKey.KeyId {
		return 0, fmt.Errorf("old_key_id %s is the current active key; cannot migrate from current key", oldKeyId)
	}

	// 4. Verify outbound is disabled for this chain
	outboundEnabled, err := k.uregistryKeeper.IsChainOutboundEnabled(ctx, chain)
	if err != nil {
		return 0, fmt.Errorf("failed to check outbound status for chain %s: %w", chain, err)
	}
	if outboundEnabled {
		return 0, fmt.Errorf("outbound is still enabled for chain %s; disable outbound before initiating migration", chain)
	}

	// 5. Verify no pending outbounds for this chain
	hasPending, err := k.uexecutorKeeper.HasPendingOutboundsForChain(ctx, chain)
	if err != nil {
		return 0, fmt.Errorf("failed to check pending outbounds for chain %s: %w", chain, err)
	}
	if hasPending {
		return 0, fmt.Errorf("chain %s still has pending outbounds; wait for them to drain before migration", chain)
	}

	// 6. Check no existing PENDING migration for this (old_key_id, chain) combo
	err = k.PendingMigrations.Walk(ctx, nil, func(migrationId uint64, _ uint64) (bool, error) {
		m, err := k.FundMigrations.Get(ctx, migrationId)
		if err != nil {
			return true, err
		}
		if m.OldKeyId == oldKeyId && m.Chain == chain {
			return true, fmt.Errorf("pending migration already exists for key %s on chain %s (migration_id: %d)",
				oldKeyId, chain, migrationId)
		}
		return false, nil
	})
	if err != nil {
		return 0, err
	}

	// 7. Fetch gas price from EVM oracle
	gasPrice, err := k.uexecutorKeeper.GetGasPriceByChain(sdkCtx, chain)
	if err != nil {
		return 0, fmt.Errorf("failed to get gas price for chain %s: %w", chain, err)
	}

	// 8. Create migration record
	migrationId, err := k.NextMigrationId.Next(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to get next migration id: %w", err)
	}

	migration := types.FundMigration{
		Id:               migrationId,
		OldKeyId:         oldKeyId,
		OldTssPubkey:     oldKey.TssPubkey,
		CurrentKeyId:     currentKey.KeyId,
		CurrentTssPubkey: currentKey.TssPubkey,
		Chain:            chain,
		Status:           types.FundMigrationStatus_FUND_MIGRATION_STATUS_PENDING,
		InitiatedBlock:   sdkCtx.BlockHeight(),
		GasPrice:         gasPrice.String(),
		GasLimit:         nativeTransferGasLimit,
	}

	if err := k.FundMigrations.Set(ctx, migrationId, migration); err != nil {
		return 0, fmt.Errorf("failed to store fund migration: %w", err)
	}
	if err := k.PendingMigrations.Set(ctx, migrationId, migrationId); err != nil {
		return 0, fmt.Errorf("failed to store pending migration index: %w", err)
	}

	// 8. Emit event
	event, err := types.NewFundMigrationInitiatedEvent(types.FundMigrationInitiatedEventData{
		MigrationID:      migrationId,
		OldKeyID:         oldKeyId,
		OldTssPubkey:     oldKey.TssPubkey,
		CurrentKeyID:     currentKey.KeyId,
		CurrentTssPubkey: currentKey.TssPubkey,
		Chain:            chain,
		BlockHeight:      sdkCtx.BlockHeight(),
		GasPrice:         gasPrice.String(),
		GasLimit:         nativeTransferGasLimit,
	})
	if err != nil {
		return 0, fmt.Errorf("failed to create migration event: %w", err)
	}
	sdkCtx.EventManager().EmitEvent(event)

	k.Logger().Info("fund migration initiated",
		"migration_id", migrationId,
		"old_key_id", oldKeyId,
		"current_key_id", currentKey.KeyId,
		"chain", chain,
	)

	return migrationId, nil
}
