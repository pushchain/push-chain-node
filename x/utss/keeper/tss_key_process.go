package keeper

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/pushchain/push-chain-node/x/utss/types"
)

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

	k.Logger().Info("TSS process finalized", "id", processID, "status", status.String())
	return nil
}

// GetTssKeyProcessByID retrieves a specific tss key process from history using process_id.
func (k Keeper) GetTssKeyProcessByID(ctx context.Context, processID uint64) (types.TssKeyProcess, bool, error) {
	key, err := k.ProcessHistory.Get(ctx, processID)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return types.TssKeyProcess{}, false, nil
		}
		return types.TssKeyProcess{}, false, err
	}
	return key, true, nil
}

// GetCurrentTssParticipants returns the participants of current tss (ongoing)
func (k Keeper) GetCurrentTssParticipants(ctx context.Context) ([]string, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	currentProcess, err := k.CurrentTssProcess.Get(ctx)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return []string{}, nil
		}
		return nil, err
	}
	if sdkCtx.BlockHeight() >= currentProcess.ExpiryHeight {
		return []string{}, nil
	}
	return currentProcess.Participants, nil
}

// HasOngoingTss returns true if a TSS process exists and is not expired
func (k Keeper) HasOngoingTss(ctx context.Context) (bool, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	currentProcess, err := k.CurrentTssProcess.Get(ctx)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return false, nil
		}
		return false, err
	}

	if sdkCtx.BlockHeight() >= currentProcess.ExpiryHeight {
		return false, nil
	}

	return true, nil
}
