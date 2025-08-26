package keeper

import (
	"context"

	errors "cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/rollchains/pchain/x/ue/types"
)

// voteInbound is for uvalidators for voting on synthetic asset inbound bridging
func (k Keeper) VoteInbound(ctx context.Context, universalValidator string, inbound types.Inbound) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	// Step 1: Check if inbound synthetic is there in the UTX
	// TODO
	// isPending, err := k.IsInUTX
	// if err != nil {
	// 	return err
	// }

	// use a temporary context to not commit any ballot state change in case of error
	tmpCtx, commit := sdkCtx.CacheContext()

	// Step 2: Add inbound synthetic to pending set - adds if not present, else does nothing
	if err := k.AddPendingInbound(tmpCtx, inbound); err != nil {
		return err
	}

	// Step 3: Vote on inbound ballot
	isFinalized, _, err := k.VoteOnInboundBallot(tmpCtx, universalValidator, inbound)
	if err != nil {
		return errors.Wrap(err, "failed to vote on inbound ballot")
	}

	commit()

	// Voting not finalized yet
	if !isFinalized {
		return nil
	}

	// Voting is finalized
	utx := types.UniversalTx{
		InboundTx:       &inbound,
		PcTx:            nil,
		OutboundTx:      nil,
		UniversalStatus: types.UniversalTxStatus_PENDING_INBOUND_EXECUTION,
	}

	universalTxKey := types.GetInboundKey(inbound)

	// Step 4: If finalized, create the UniversalTx
	if err := k.CreateUniversalTx(ctx, universalTxKey, utx); err != nil {
		return err
	}

	// Step 5: Remove from pending inbound set
	if err := k.RemovePendingInbound(ctx, inbound); err != nil {
		return err
	}

	// Step 6: Execution
	if err := k.ExecuteInbound(ctx, utx); err != nil {
		return err
	}

	return nil
}
