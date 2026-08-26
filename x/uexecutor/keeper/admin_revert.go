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
// ballot can no longer finalize. The revert outbound enters the normal
// PendingOutbounds flow; UVs sign it via TSS and broadcast it to the source
// chain, refunding the user.
//
// Precondition: the ballot for the supplied inbound must be either
//
//   - EXPIRED, or
//   - PENDING but provably unreachable - every eligible voter has already voted
//     (Ballot.IsUnreachablePending).
//
// The second case exists because RecomputeBallotQuorum can leave a ballot
// permanently stuck (F-2026-18147). It preserves the votes of still-eligible
// voters, lowers the threshold, and returns PENDING without ever calling
// CheckIfFinalizingVote. If the preserved votes already fill every slot there is
// no vote left to cast - Ballot.AddVote rejects repeat votes - so nothing can
// move the ballot off PENDING and the deposit sits in the source gateway
// forever. Such a ballot is terminal in fact whatever its stored status says, so
// the hatch treats it as terminal too. This holds for both stuck shapes: YES
// already at or above the recomputed threshold (should have passed, never will)
// and YES below it (can never reach it).
//
// The deliberate limits of that widening:
//
//   - A PENDING ballot with an unvoted eligible voter is still refused. It can
//     finalize normally, and reverting would race a legitimate vote.
//   - A PENDING ballot with no eligible voters at all is refused too. Recompute
//     rebuilds the voter list from the live UV set, so it either gains real
//     voters or auto-expires; a shipped path already resolves it.
//   - Fixing this inside RecomputeBallotQuorum by calling CheckIfFinalizingVote
//     was rejected. That marks the ballot PASSED without running VoteInbound's
//     post-finalization pipeline, so no UniversalTx is ever built
//     (msg_vote_inbound.go builds one only when that specific vote finalizes)
//     and BallotHooks returns early on PASSED without minting or executing. The
//     funds would stay stuck AND the ballot would no longer be PENDING, so
//     recompute could not be retried - strictly worse than leaving it alone.
//
// This route reverts rather than executes: the user is refunded on the source
// chain instead of receiving bridged funds on Push. For a ballot whose YES votes
// met the threshold that is the less generous of the two resolutions, and it is
// the deliberate trade for a change that stays inside the module that owns
// inbound execution.
//
// The ballot record itself is left untouched. The HasUniversalTx guard below is
// the idempotency barrier, and mutating ballot status from x/uexecutor would
// fire the uvalidator terminal hook and re-enter inbound routing for an inbound
// this call is already resolving.
//
// REJECTED stays refused, deliberately and not by omission (F-2026-18801): a
// supermajority affirmatively voted that the observation is invalid, so a revert
// outbound would pay real funds out of the TSS-controlled vault against a
// deposit the validator set concluded never happened. PENDING-unreachable is the
// opposite situation - nobody can act at all - which is why it is accepted while
// REJECTED is not.
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

	var revertReason string
	switch {
	case ballot.Status == uvalidatortypes.BallotStatus_BALLOT_STATUS_EXPIRED:
		revertReason = "admin revert: stuck ballot expired"
	case ballot.IsUnreachablePending():
		revertReason = "admin revert: pending ballot unreachable, every eligible voter has already voted"
	default:
		return "", "", errors.Wrap(sdkErrors.ErrInvalidRequest,
			fmt.Sprintf("ballot %s status is %s; admin revert requires EXPIRED, or PENDING with every eligible voter already voted (no further vote can be cast). "+
				"MsgRecomputeBallotQuorum rebuilds the eligible-voter set from the live UV set and marks the ballot EXPIRED only when zero eligible voters remain, "+
				"so a pending ballot that still has an unvoted eligible voter has to be finalized by that voter through the normal vote flow",
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
			ErrorMsg: revertReason,
		}},
	}
	if cErr := k.CreateUniversalTx(ctx, universalTxKey, utx); cErr != nil {
		return "", "", fmt.Errorf("failed to create utx for revert: %w", cErr)
	}

	revertOutbound := k.buildRevertOutbound(sdkCtx, &inbound)
	if revertOutbound == nil {
		return "", "", fmt.Errorf("failed to build revert outbound for inbound %s", universalTxKey)
	}

	if attachErr := k.attachOutboundsToUtx(sdkCtx, universalTxKey, []*types.OutboundTx{revertOutbound}, revertReason); attachErr != nil {
		return "", "", fmt.Errorf("failed to attach revert outbound: %w", attachErr)
	}

	k.Logger().Info("admin revert: inbound revert outbound created",
		"utx_id", universalTxKey,
		"outbound_id", revertOutbound.Id,
		"source_chain", inbound.SourceChain,
		"recipient", revertOutbound.Recipient,
		"amount", revertOutbound.Amount,
		"ballot_status", ballot.Status.String(),
		"reason", revertReason,
	)

	return universalTxKey, revertOutbound.Id, nil
}
