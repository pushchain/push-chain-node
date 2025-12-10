package keeper

import (
	"context"
	"fmt"

	errors "cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/pushchain/push-chain-node/x/utss/types"
	uvalidatortypes "github.com/pushchain/push-chain-node/x/uvalidator/types"
)

func (k Keeper) VoteTssKeyProcess(
	ctx context.Context,
	universalValidator sdk.ValAddress,
	tssPubKey, keyId string,
	processId uint64,
) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	process, err := k.CurrentTssProcess.Get(ctx)
	if err != nil {
		return fmt.Errorf("no active TSS process running")
	}

	if sdkCtx.BlockHeight() >= process.ExpiryHeight {
		return fmt.Errorf("TSS process %d has expired", process.Id)
	}

	// Step 1: Ensure the key doesn't already exist
	_, found, err := k.GetTssKeyByID(ctx, keyId)
	if err != nil {
		return errors.Wrap(err, "failed to check existing TSS key")
	}
	if found {
		return fmt.Errorf("tss key with key_id %s already exists", keyId)
	}

	// Step 2: Vote on the ballot (using a cache context so we don’t mutate state on failure)
	tmpCtx, commit := sdkCtx.CacheContext()
	isFinalized, _, err := k.VoteOnTssBallot(tmpCtx, universalValidator, processId, tssPubKey, keyId)
	if err != nil {
		return errors.Wrap(err, "failed to vote on TSS ballot")
	}

	// Commit the vote
	commit()

	// Step 3: If not finalized yet, do nothing
	if !isFinalized {
		return nil
	}

	process.Status = types.TssKeyProcessStatus_TSS_KEY_PROCESS_SUCCESS

	// Step 5: Ballot finalized — create the TssKey record
	tssKey := types.TssKey{
		TssPubkey:            tssPubKey,
		KeyId:                keyId,
		Participants:         process.Participants,
		FinalizedBlockHeight: sdkCtx.BlockHeight(),
		KeygenBlockHeight:    process.BlockHeight,
		ProcessId:            processId,
	}

	// Step 6: Store updates
	if err := k.CurrentTssKey.Set(ctx, tssKey); err != nil {
		return errors.Wrap(err, "failed to set current TSS key")
	}
	if err := k.TssKeyHistory.Set(ctx, keyId, tssKey); err != nil {
		return errors.Wrap(err, "failed to store TSS key history")
	}
	if err := k.FinalizeTssKeyProcess(ctx, processId, types.TssKeyProcessStatus_TSS_KEY_PROCESS_SUCCESS); err != nil {
		return errors.Wrap(err, "failed to finalise TSS process")
	}

	// Only KEYGEN and QUORUM_CHANGE update validator lifecycle
	if process.ProcessType == types.TssProcessType_TSS_PROCESS_KEYGEN ||
		process.ProcessType == types.TssProcessType_TSS_PROCESS_QUORUM_CHANGE {

		universalValidatorSet, err := k.uvalidatorKeeper.GetEligibleVoters(ctx)
		if err != nil {
			return err
		}

		for i := range universalValidatorSet {
			uv := &universalValidatorSet[i]
			coreValidatorAddress := uv.IdentifyInfo.CoreValidatorAddress

			foundInParticipants := false
			for _, participant := range tssKey.Participants {
				if participant == coreValidatorAddress {
					foundInParticipants = true
					break
				}
			}

			valAddr, err := sdk.ValAddressFromBech32(coreValidatorAddress)
			if err != nil {
				return err
			}

			// update pending_join validator to active
			switch uv.LifecycleInfo.CurrentStatus {
			case uvalidatortypes.UVStatus_UV_STATUS_PENDING_JOIN:
				if foundInParticipants {
					uv.LifecycleInfo.CurrentStatus = uvalidatortypes.UVStatus_UV_STATUS_ACTIVE
					if err := k.uvalidatorKeeper.UpdateValidatorStatus(ctx, valAddr, uvalidatortypes.UVStatus_UV_STATUS_ACTIVE); err != nil {
						k.logger.Error("failed to activate universal validator", "valAddr", coreValidatorAddress, "err", err)
					}
				}
			// update pending_leave validator to inactive
			case uvalidatortypes.UVStatus_UV_STATUS_PENDING_LEAVE:
				if !foundInParticipants {
					uv.LifecycleInfo.CurrentStatus = uvalidatortypes.UVStatus_UV_STATUS_INACTIVE
					if err := k.uvalidatorKeeper.UpdateValidatorStatus(ctx, valAddr, uvalidatortypes.UVStatus_UV_STATUS_INACTIVE); err != nil {
						k.logger.Error("failed to inactivate universal validator", "valAddr", coreValidatorAddress, "err", err)
					}
				}
			}
		}
	}

	// Step 7: Emit finalized event
	event, _ := types.NewTssKeyFinalizedEvent(types.TssKeyFinalizedEvent{
		ProcessID: processId,
		KeyID:     keyId,
		TssPubKey: tssPubKey,
	})
	sdkCtx.EventManager().EmitEvent(event)

	k.logger.Info(
		"TSS key finalized",
		"key_id", keyId,
		"process_id", processId,
		"pubkey", tssPubKey,
	)

	return nil
}
