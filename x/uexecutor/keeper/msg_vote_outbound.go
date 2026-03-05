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

	commit()

	// Step 4: Exit if not finalized yet
	if !isFinalized {
		return nil
	}

	// Step 5: Update outbound state to OBSERVED
	outbound.OutboundStatus = types.Status_OBSERVED
	outbound.ObservedTx = &observedTx

	// Persist the state inside UniversalTx
	if err := k.UpdateOutbound(ctx, utxId, outbound); err != nil {
		return err
	}

	// Step 6: Finalize outbound (refund if failed) - Don't return error
	_ = k.FinalizeOutbound(ctx, utxId, outbound)

	return nil
}
