package keeper

import (
	"context"
	"fmt"

	errors "cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/pushchain/push-chain-node/x/utss/types"
)

func (k Keeper) VoteTssKeyProcess(
	ctx context.Context,
	universalValidator sdk.ValAddress,
	processId uint64,
	tssPubKey, keyId string,
) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

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
	isFinalized, _, err := k.VoteOnTssBallot(tmpCtx, universalValidator, processId, keyId)
	if err != nil {
		return errors.Wrap(err, "failed to vote on TSS ballot")
	}

	// Commit the vote
	commit()

	// Step 3: If not finalized yet, do nothing
	if !isFinalized {
		return nil
	}

	// Step 4: Mark process as successful
	process, found, err := k.GetTssKeyProcessByID(ctx, processId)
	if err != nil {
		return errors.Wrap(err, "failed to fetch TSS process")
	}
	if !found {
		return fmt.Errorf("TSS process %d not found", processId)
	}
	process.Status = types.TssKeyProcessStatus_TSS_KEY_PROCESS_SUCCESS

	// Step 5: Ballot finalized — create the TssKey record
	tssKey := types.TssKey{
		TssPubkey:            tssPubKey,
		KeyId:                keyId,
		Participants:         []string{universalValidator.String()},
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
	if err := k.CurrentTssProcess.Remove(ctx); err != nil {
		return errors.Wrap(err, "failed to clear current TSS process")
	}
	if err := k.ProcessHistory.Set(ctx, processId, process); err != nil {
		return errors.Wrap(err, "failed to archive TSS process")
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
