package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkErrors "github.com/cosmos/cosmos-sdk/types/errors"

	"github.com/pushchain/push-chain-node/x/uexecutor/types"
	uvalidatortypes "github.com/pushchain/push-chain-node/x/uvalidator/types"
)

// RevertStuckInbound creates an INBOUND_REVERT outbound for an inbound whose
// ballot has expired without finalizing. The revert outbound enters the normal
// PendingOutbounds flow; UVs sign it via TSS and broadcast it to the source
// chain, refunding the user.
//
// Strict precondition: the ballot for the supplied inbound must be in EXPIRED
// state. Admin must run MsgRecomputeBallotQuorum first to drive a stuck ballot
// to EXPIRED if it isn't already (recompute auto-expires when no eligible
// voters remain).
//
// Returns the new UTX ID and revert outbound ID for telemetry.
func (k Keeper) RevertStuckInbound(ctx context.Context, inbound types.Inbound) (utxId, outboundId string, err error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Same canonical form as the vote path, so the admin-supplied payload
	// derives the same ballot key / UTX key the votes did.
	inbound.Canonicalize()

	if vErr := inbound.ValidateBasic(); vErr != nil {
		return "", "", errors.Wrap(sdkErrors.ErrInvalidRequest, vErr.Error())
	}

	ballotKey, err := types.GetInboundBallotKey(inbound)
	if err != nil {
		return "", "", errors.Wrap(sdkErrors.ErrInvalidRequest, fmt.Sprintf("failed to derive ballot key: %s", err))
	}

	ballot, err := k.uvalidatorKeeper.GetBallot(ctx, ballotKey)
	if err != nil {
		return "", "", errors.Wrap(sdkErrors.ErrNotFound, fmt.Sprintf("ballot for inbound not found (key=%s): %s", ballotKey, err))
	}

	if ballot.Status != uvalidatortypes.BallotStatus_BALLOT_STATUS_EXPIRED {
		return "", "", errors.Wrap(sdkErrors.ErrInvalidRequest,
			fmt.Sprintf("ballot %s status is %s; admin revert requires EXPIRED (use MsgRecomputeBallotQuorum to drive a stuck pending ballot to EXPIRED)",
				ballotKey, ballot.Status.String()))
	}

	universalTxKey := types.GetInboundUniversalTxKey(inbound)
	if has, hErr := k.HasUniversalTx(ctx, universalTxKey); hErr != nil {
		return "", "", fmt.Errorf("failed to check utx existence: %w", hErr)
	} else if has {
		return "", "", errors.Wrap(sdkErrors.ErrInvalidRequest,
			fmt.Sprintf("universal tx %s already exists for this inbound", universalTxKey))
	}

	utx := types.UniversalTx{
		Id:        universalTxKey,
		InboundTx: &inbound,
		PcTx: []*types.PCTx{{
			Status:   "FAILED",
			ErrorMsg: "admin revert: stuck ballot expired",
		}},
	}
	if cErr := k.CreateUniversalTx(ctx, universalTxKey, utx); cErr != nil {
		return "", "", fmt.Errorf("failed to create utx for revert: %w", cErr)
	}

	revertOutbound := k.buildRevertOutbound(sdkCtx, &inbound)
	if revertOutbound == nil {
		return "", "", fmt.Errorf("failed to build revert outbound for inbound %s", universalTxKey)
	}

	if attachErr := k.attachOutboundsToUtx(sdkCtx, universalTxKey, []*types.OutboundTx{revertOutbound}, "admin revert: stuck ballot expired"); attachErr != nil {
		return "", "", fmt.Errorf("failed to attach revert outbound: %w", attachErr)
	}

	k.Logger().Info("admin revert: inbound revert outbound created",
		"utx_id", universalTxKey,
		"outbound_id", revertOutbound.Id,
		"source_chain", inbound.SourceChain,
		"recipient", revertOutbound.Recipient,
		"amount", revertOutbound.Amount,
	)

	return universalTxKey, revertOutbound.Id, nil
}
