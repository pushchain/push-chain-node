package keeper

import (
	"context"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/pushchain/push-chain-node/x/uexecutor/types"
)

// VoteOutbound is for uvalidators for voting on observed outbound tx on external chain
func (k Keeper) VoteOutbound(
	ctx context.Context,
	universalValidator sdk.ValAddress,
	utxId string,
	outboundId string,
	observedTx types.OutboundObservation,
) error {
	k.Logger().Info("vote outbound received",
		"validator", universalValidator.String(),
		"utx_id", utxId,
		"outbound_id", outboundId,
		"obs_success", observedTx.Success,
	)

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Step 1: Fetch UniversalTx
	utx, found, err := k.GetUniversalTx(ctx, utxId)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("UniversalTx not found: %s", utxId)
	}
	if utx.OutboundTx == nil {
		return fmt.Errorf("no outbound tx found in UniversalTx %s", utxId)
	}

	// Step 2: Find outbound by id
	var outbound types.OutboundTx
	found = false
	for _, ob := range utx.OutboundTx {
		if ob.Id == outboundId {
			outbound = *ob
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("outbound %s not found in UniversalTx %s", outboundId, utxId)
	}

	// Prevent double-finalization
	if outbound.OutboundStatus != types.Status_PENDING {
		k.Logger().Warn("vote outbound rejected: outbound already finalized",
			"outbound_id", outboundId,
			"status", outbound.OutboundStatus.String(),
		)
		return fmt.Errorf("outbound with key %s is already finalized", outboundId)
	}

	// Use temp context to prevent partial writes
	tmpCtx, commit := sdkCtx.CacheContext()

	// Step 3: Vote on outbound ballot
	isFinalized, _, err := k.VoteOnOutboundBallot(
		tmpCtx,
		universalValidator,
		utxId,
		outboundId,
		observedTx,
	)
	if err != nil {
		return err
	}

	// Step 3b: Record this validator's vote in the per-outbound PendingOutbounds
	// entry (variant-aware audit trail). Each unique ObservedTx payload becomes
	// its own variant; multiple variants per outbound_id indicate validator
	// divergence on the destination-chain observation.
	ballotKey, err := types.GetOutboundBallotKey(utxId, outboundId, observedTx)
	if err != nil {
		return fmt.Errorf("failed to derive outbound ballot key: %w", err)
	}
	if err := k.RecordOutboundVote(tmpCtx, outboundId, observedTx, universalValidator.String(), ballotKey); err != nil {
		return err
	}

	commit()

	// Step 4: Exit if not finalized yet
	if !isFinalized {
		k.Logger().Debug("vote outbound recorded, ballot not yet finalized",
			"validator", universalValidator.String(),
			"utx_id", utxId,
			"outbound_id", outboundId,
		)
		return nil
	}

	// Step 5: Update outbound state to OBSERVED
	outbound.OutboundStatus = types.Status_OBSERVED
	outbound.ObservedTx = &observedTx

	k.Logger().Info("outbound observed",
		"utx_id", utxId,
		"outbound_id", outboundId,
		"success", observedTx.Success,
		"dest_chain", outbound.DestinationChain,
	)

	// Persist the state inside UniversalTx
	if err := k.UpdateOutbound(ctx, utxId, outbound); err != nil {
		return err
	}

	// Remove from pending outbounds index now that status is OBSERVED
	if err := k.PendingOutbounds.Remove(ctx, outboundId); err != nil {
		return fmt.Errorf("failed to remove pending outbound index for %s: %w", outboundId, err)
	}

	// Step 6: Finalize outbound (refund if failed).
	// If re-mint fails, handleFailedOutbound marks it ABORTED internally and returns nil.
	// Business logic errors are stored in RevertError on the UTX; only infra errors are returned.
	if err := k.FinalizeOutbound(ctx, utxId, outbound); err != nil {
		k.Logger().Error("outbound finalization error stored on utx",
			"utx_id", utxId,
			"outbound_id", outboundId,
			"error", err.Error(),
		)
		if storeErr := k.UpdateUniversalTx(ctx, utxId, func(u *types.UniversalTx) error {
			u.RevertError = err.Error()
			return nil
		}); storeErr != nil {
			return storeErr
		}
	}

	return nil
}
