package keeper

import (
	"context"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/pushchain/push-chain-node/x/utss/types"
	uvalidatortypes "github.com/pushchain/push-chain-node/x/uvalidator/types"
)

// InitiateTssKeyProcess creates a new keygen or reshare process.
func (k Keeper) InitiateTssKeyProcess(
	ctx context.Context,
	processType types.TssProcessType,
) error {

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	currentHeight := sdkCtx.BlockHeight()

	// Check if a current process exists and is still active (not expired and pending)
	existing, err := k.CurrentTssProcess.Get(ctx)
	if err == nil {
		if currentHeight < existing.ExpiryHeight {
			// Expire the existing process
			k.Logger().Info("Validator set changed: force-expiring current TSS process",
				"old_process_id", existing.Id,
				"old_expiry", existing.ExpiryHeight,
				"current_height", currentHeight)

			existing.ExpiryHeight = currentHeight - 1

			// Store as current
			if err := k.CurrentTssProcess.Set(ctx, existing); err != nil {
				return fmt.Errorf("failed to set current process: %w", err)
			}

			// Add to history
			if err := k.ProcessHistory.Set(ctx, existing.Id, existing); err != nil {
				return fmt.Errorf("failed to store process history: %w", err)
			}
		}
	}

	// Generate new process ID
	processID, err := k.NextProcessId.Next(ctx)
	if err != nil {
		return fmt.Errorf("failed to generate process id: %w", err)
	}

	participants, err := k.GetTssParticipants(ctx, processType)
	if err != nil {
		return fmt.Errorf("failed to get tss participants: %w", err)
	}

	// Create a new process
	process := types.TssKeyProcess{
		Status:       types.TssKeyProcessStatus_TSS_KEY_PROCESS_PENDING,
		Participants: participants,
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
		Participants: process.Participants,
		ExpiryHeight: process.ExpiryHeight,
	})
	if err != nil {
		return err
	}

	sdkCtx.EventManager().EmitEvent(event)

	k.Logger().Info("New TSS process initiated",
		"id", process.Id,
		"type", process.ProcessType,
		"participants", len(process.Participants),
		"expiry_height", process.ExpiryHeight,
	)

	return nil
}

func (k Keeper) GetTssParticipants(ctx context.Context, processType types.TssProcessType) ([]string, error) {
	switch processType {
	case types.TssProcessType_TSS_PROCESS_KEYGEN, types.TssProcessType_TSS_PROCESS_QUORUM_CHANGE:
		// in keygen or qc, participants will be active or pending_join
		universalValidators, err := k.uvalidatorKeeper.GetEligibleVoters(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch eligible validators: %w", err)
		}

		// Convert []sdk.ValAddress -> []string
		participants := make([]string, len(universalValidators))
		for i, v := range universalValidators {
			participants[i] = v.IdentifyInfo.CoreValidatorAddress
		}
		return participants, nil
	case types.TssProcessType_TSS_PROCESS_REFRESH:
		// in key refresh, participants will be active or pending_leave
		universalValidators, err := k.uvalidatorKeeper.GetAllUniversalValidators(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch all universal validators: %w", err)
		}

		var participants []string
		for _, v := range universalValidators {
			status := v.LifecycleInfo.CurrentStatus
			if status == uvalidatortypes.UVStatus_UV_STATUS_ACTIVE ||
				status == uvalidatortypes.UVStatus_UV_STATUS_PENDING_LEAVE {
				participants = append(participants, v.IdentifyInfo.CoreValidatorAddress)
			}
		}
		return participants, nil
	default:
		return nil, fmt.Errorf("unsupported TSS process type: %s", processType.String())
	}
}
