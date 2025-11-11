package keeper

import (
	"context"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/pushchain/push-chain-node/x/utss/types"
)

// InitiateTssKeyProcess creates a new keygen or reshare process.
func (k Keeper) InitiateTssKeyProcess(
	ctx context.Context,
	processType types.TssProcessType,
	expiryHeight int64,
	participants []string,
) (types.TssKeyProcess, error) {

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Check if a current process exists and is still active (not expired and pending)
	existing, err := k.CurrentTssProcess.Get(ctx)
	if err == nil {
		if sdkCtx.BlockHeight() < existing.ExpiryHeight &&
			existing.Status == types.TssKeyProcessStatus_TSS_KEY_PROCESS_PENDING {
			return types.TssKeyProcess{}, fmt.Errorf("an active TSS process already exists (id: %d)", existing.Id)
		}
	}

	// Generate new process ID
	processID, err := k.NextProcessId.Next(ctx)
	if err != nil {
		return types.TssKeyProcess{}, fmt.Errorf("failed to generate process id: %w", err)
	}

	// Create a new process
	process := types.TssKeyProcess{
		Status:       types.TssKeyProcessStatus_TSS_KEY_PROCESS_PENDING,
		Participants: participants,
		BlockHeight:  sdkCtx.BlockHeight(),
		ExpiryHeight: expiryHeight,
		ProcessType:  processType,
		Id:           processID,
	}

	if err := process.ValidateBasic(); err != nil {
		return types.TssKeyProcess{}, fmt.Errorf("invalid tss process: %w", err)
	}

	// Store as current
	if err := k.CurrentTssProcess.Set(ctx, process); err != nil {
		return types.TssKeyProcess{}, fmt.Errorf("failed to set current process: %w", err)
	}

	// Add to history
	if err := k.ProcessHistory.Set(ctx, process.Id, process); err != nil {
		return types.TssKeyProcess{}, fmt.Errorf("failed to store process history: %w", err)
	}

	k.Logger().Info("🚀 New TSS process initiated",
		"id", process.Id,
		"type", process.ProcessType,
		"participants", len(participants),
	)

	return process, nil
}

// FinalizeTssKeyProcess updates a process status and removes it from current if completed.
func (k Keeper) FinalizeTssKeyProcess(ctx context.Context, processID uint64, status types.TssKeyProcessStatus) error {
	process, err := k.ProcessHistory.Get(ctx, processID)
	if err != nil {
		return fmt.Errorf("tss process %d not found: %w", processID, err)
	}

	process.Status = status
	if err := k.ProcessHistory.Set(ctx, processID, process); err != nil {
		return fmt.Errorf("failed to update process: %w", err)
	}

	if status != types.TssKeyProcessStatus_TSS_KEY_PROCESS_PENDING {
		if err := k.CurrentTssProcess.Remove(ctx); err != nil {
			k.Logger().Error("failed to clear current process", "err", err)
		}
	}

	k.Logger().Info("✅ TSS process finalized", "id", processID, "status", status.String())
	return nil
}
