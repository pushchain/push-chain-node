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
) error {

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Check if a current process exists and is still active (not expired and pending)
	existing, err := k.CurrentTssProcess.Get(ctx)
	if err == nil {
		if sdkCtx.BlockHeight() < existing.ExpiryHeight {
			return fmt.Errorf("an active TSS process already exists (id: %d)", existing.Id)
		}
	}

	// Generate new process ID
	processID, err := k.NextProcessId.Next(ctx)
	if err != nil {
		return fmt.Errorf("failed to generate process id: %w", err)
	}

	// initiate a tss key process only for those validators which are either pending_join or active
	universalValidators, err := k.uvalidatorKeeper.GetEligibleVoters(ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch eligible validators: %w", err)
	}

	// Convert []sdk.ValAddress -> []string
	universalValidatorSetStrs := make([]string, len(universalValidators))
	for i, v := range universalValidators {
		universalValidatorSetStrs[i] = v.IdentifyInfo.CoreValidatorAddress
	}

	// Create a new process
	process := types.TssKeyProcess{
		Status:       types.TssKeyProcessStatus_TSS_KEY_PROCESS_PENDING,
		Participants: universalValidatorSetStrs,
		BlockHeight:  sdkCtx.BlockHeight(),
		ExpiryHeight: sdkCtx.BlockHeight() + int64(types.DefaultTssProcessExpiryAfterBlocks),
		ProcessType:  processType,
		Id:           processID,
	}

	if err := process.ValidateBasic(); err != nil {
		return fmt.Errorf("invalid tss process: %w", err)
	}

	// Store as current
	if err := k.CurrentTssProcess.Set(ctx, process); err != nil {
		return fmt.Errorf("failed to set current process: %w", err)
	}

	// Add to history
	if err := k.ProcessHistory.Set(ctx, process.Id, process); err != nil {
		return fmt.Errorf("failed to store process history: %w", err)
	}

	// Emit TSS Process Initiated Event
	event, err := types.NewTssProcessInitiatedEvent(types.TssProcessInitiatedEvent{
		ProcessID:    process.Id,
		ProcessType:  process.ProcessType.String(),
		Participants: len(process.Participants),
		ExpiryHeight: process.ExpiryHeight,
	})
	if err != nil {
		return err
	}

	sdkCtx.EventManager().EmitEvent(event)

	k.Logger().Info("New TSS process initiated",
		"id", process.Id,
		"type", process.ProcessType,
		"participants", len(universalValidatorSetStrs),
		"expiry_height", process.ExpiryHeight,
	)

	return nil
}
