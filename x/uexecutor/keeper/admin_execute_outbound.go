package keeper

import (
	"context"
	"fmt"
	"strings"

	"cosmossdk.io/errors"
	sdkErrors "github.com/cosmos/cosmos-sdk/types/errors"

	"github.com/pushchain/push-chain-node/utils"
	"github.com/pushchain/push-chain-node/x/uexecutor/types"
	uvalidatortypes "github.com/pushchain/push-chain-node/x/uvalidator/types"
)

// ExecuteStuckOutbound settles an outbound whose ballot can no longer reach a
// terminal-and-settled state, running the same pipeline a finalizing vote would
// have run.
//
// All post-finalization work for an outbound lives inside VoteOutbound, so an
// outbound whose ballot never finalizes has no recovery path at all: the tokens
// were already burned on Push, the destination-chain tx may well have landed,
// and nothing marks the outbound OBSERVED, refunds gas or mints the funds back.
// Two shapes get stuck:
//
//   - PENDING-unreachable. Every eligible voter has voted but the stored
//     VotingThreshold is stale/high - what MsgRecomputeBallotQuorum can leave
//     behind (F-2026-18147). AddVote rejects repeat votes, so nothing can move
//     the ballot.
//   - EXPIRED. Quorum never formed.
//
// Both are accepted, and the settlement outcome follows observed_tx.success in
// both: a success settles normally, a failure mints the bridged tokens back to
// the revert recipient and refunds the excess gas. One message covers
// settle-and-refund because the outcome is carried in the observation, not in
// the message type. That is why EXPIRED is accepted here while
// MsgExecuteStuckInbound refuses it: an inbound has MsgRevertStuckInbound as the
// honest resolution for "quorum never formed", whereas an outbound has no
// sibling hatch - the funds have already left Push and only the observation says
// what became of them.
//
// Deriving the ballot key from the admin-supplied observation is the security
// property: an observation no validator ever voted derives a key with no ballot,
// so the admin can only settle against something the validator set actually
// reported. For PENDING-unreachable that is a full threshold of votes; for
// EXPIRED it is at least one, and picking the right variant among any divergent
// observations is the admin's call.
//
// Returns the settled outbound ID for telemetry.
func (k Keeper) ExecuteStuckOutbound(
	ctx context.Context,
	utxId string,
	outboundId string,
	observedTx types.OutboundObservation,
) (string, error) {
	// Locate the outbound first, exactly as VoteOutbound does: the observed tx
	// hash is canonicalized for the destination chain, which is read off it.
	utx, found, err := k.GetUniversalTx(ctx, utxId)
	if err != nil {
		return "", err
	}
	if !found {
		return "", errors.Wrap(sdkErrors.ErrNotFound, fmt.Sprintf("UniversalTx not found: %s", utxId))
	}
	if utx.OutboundTx == nil {
		return "", errors.Wrap(sdkErrors.ErrNotFound, fmt.Sprintf("no outbound tx found in UniversalTx %s", utxId))
	}

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
		return "", errors.Wrap(sdkErrors.ErrNotFound, fmt.Sprintf("outbound %s not found in UniversalTx %s", outboundId, utxId))
	}

	// Same canonical form as the vote path, and applied before the ballot key is
	// derived: the key is a digest over these exact fields, so canonicalizing
	// differently or later derives a key no validator ever voted on.
	observedTx.TxHash = utils.LenientCanonicalizeTxHash(outbound.DestinationChain, observedTx.TxHash)
	observedTx.GasFeeUsed = strings.TrimSpace(observedTx.GasFeeUsed)
	observedTx.ErrorMsg = strings.TrimSpace(observedTx.ErrorMsg)

	if vErr := observedTx.ValidateBasic(); vErr != nil {
		return "", errors.Wrap(sdkErrors.ErrInvalidRequest, vErr.Error())
	}

	ballotKey, err := types.GetOutboundBallotKey(utxId, outboundId, observedTx)
	if err != nil {
		return "", errors.Wrap(sdkErrors.ErrInvalidRequest, fmt.Sprintf("failed to derive ballot key: %s", err))
	}

	ballot, err := k.uvalidatorKeeper.GetBallot(ctx, ballotKey)
	if err != nil {
		return "", errors.Wrap(sdkErrors.ErrNotFound, fmt.Sprintf("ballot for outbound not found (key=%s): %s", ballotKey, err))
	}

	// EXPIRED is already terminal; PENDING-unreachable is terminal in fact but
	// not in the record, so only that one needs the ballot driven to PASSED.
	finalizeBallot := false
	if ballot.Status != uvalidatortypes.BallotStatus_BALLOT_STATUS_EXPIRED {
		if gErr := requireCarriedUnreachablePending(ballotKey, ballot); gErr != nil {
			return "", errors.Wrap(sdkErrors.ErrInvalidRequest,
				fmt.Sprintf("%s. Admin execute of an outbound requires an EXPIRED ballot, or a PENDING one every eligible voter has already voted on whose YES votes meet the threshold",
					gErr))
		}
		finalizeBallot = true
	}

	// Idempotency barrier. An EXPIRED ballot is deliberately left untouched
	// below, so the outbound's own status is the only record of settlement.
	if outbound.OutboundStatus != types.Status_PENDING {
		return "", errors.Wrap(sdkErrors.ErrInvalidRequest,
			fmt.Sprintf("outbound with key %s is already finalized (status %s)", outboundId, outbound.OutboundStatus.String()))
	}

	// MarkBallotFinalized only accepts PASSED/REJECTED, so an EXPIRED ballot is
	// left exactly as it is - it is terminal already, and the outbound's OBSERVED
	// status is the real record. Mirrors RevertStuckInbound.
	if finalizeBallot {
		if fErr := k.uvalidatorKeeper.MarkBallotFinalized(ctx, ballotKey, uvalidatortypes.BallotStatus_BALLOT_STATUS_PASSED); fErr != nil {
			return "", fmt.Errorf("failed to finalize ballot %s: %w", ballotKey, fErr)
		}
	}

	yes, _ := ballot.CountVotes()
	k.Logger().Info("admin execute: settling stuck outbound",
		"utx_id", utxId,
		"outbound_id", outboundId,
		"ballot_id", ballotKey,
		"ballot_status", ballot.Status.String(),
		"ballot_finalized", finalizeBallot,
		"yes_votes", yes,
		"voting_threshold", ballot.VotingThreshold,
		"eligible_voters", len(ballot.EligibleVoters),
		"dest_chain", outbound.DestinationChain,
		"success", observedTx.Success,
	)

	if settleErr := k.finalizeOutboundAndSettle(ctx, utxId, outboundId, outbound, observedTx); settleErr != nil {
		return "", settleErr
	}

	return outboundId, nil
}
