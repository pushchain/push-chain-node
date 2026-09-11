package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/errors"
	sdkErrors "github.com/cosmos/cosmos-sdk/types/errors"

	"github.com/pushchain/push-chain-node/x/uexecutor/types"
	uvalidatortypes "github.com/pushchain/push-chain-node/x/uvalidator/types"
)

// ExecuteStuckInbound finalizes a stuck inbound ballot as PASSED and runs the
// pipeline a finalizing vote would have, so the user receives the funds.
//
// Sibling of RevertStuckInbound, which is the wrong resolution when a ballot's
// preserved YES votes already clear the recomputed threshold — RecomputeBallotQuorum
// leaves those PENDING forever, and a refund was the only hatch (F-2026-18147).
//
// Requires PENDING-unreachable with YES >= VotingThreshold. EXPIRED belongs to
// RevertStuckInbound: no quorum ever formed, so there is nothing to act on.
//
// The ballot key is derived from the supplied inbound, so the admin cannot
// execute anything other than the payload the validators voted on.
func (k Keeper) ExecuteStuckInbound(ctx context.Context, inbound types.Inbound) (utxId string, err error) {
	// Same canonical form as the vote path, so the admin-supplied payload
	// derives the same ballot key / UTX key the votes did.
	inbound.Canonicalize()

	if vErr := inbound.ValidateBasic(); vErr != nil {
		return "", errors.Wrap(sdkErrors.ErrInvalidRequest, vErr.Error())
	}

	ballotKey, err := types.GetInboundBallotKey(inbound)
	if err != nil {
		return "", errors.Wrap(sdkErrors.ErrInvalidRequest, fmt.Sprintf("failed to derive ballot key: %s", err))
	}

	ballot, err := k.uvalidatorKeeper.GetBallot(ctx, ballotKey)
	if err != nil {
		return "", errors.Wrap(sdkErrors.ErrNotFound, fmt.Sprintf("ballot for inbound not found (key=%s): %s", ballotKey, err))
	}

	if gErr := requireCarriedUnreachablePending(ballotKey, ballot); gErr != nil {
		return "", errors.Wrap(sdkErrors.ErrInvalidRequest,
			fmt.Sprintf("%s. An EXPIRED ballot never reached quorum at all - use MsgRevertStuckInbound to refund this inbound on the source chain instead",
				gErr))
	}

	universalTxKey := types.GetInboundUniversalTxKey(inbound)
	if has, hErr := k.HasUniversalTx(ctx, universalTxKey); hErr != nil {
		return "", fmt.Errorf("failed to check utx existence: %w", hErr)
	} else if has {
		return "", errors.Wrap(sdkErrors.ErrInvalidRequest,
			fmt.Sprintf("universal tx %s already exists for this inbound", universalTxKey))
	}

	// Finalize before executing, mirroring the vote path's ordering: uvalidator
	// marks the ballot PASSED (firing the PendingInbounds bookkeeping hook) and
	// only then does the post-finalization pipeline run. The hook may clear the
	// pending entry the pipeline would otherwise clear; that removal is a no-op
	// on an absent key.
	if fErr := k.uvalidatorKeeper.MarkBallotFinalized(ctx, ballotKey, uvalidatortypes.BallotStatus_BALLOT_STATUS_PASSED); fErr != nil {
		return "", fmt.Errorf("failed to finalize ballot %s: %w", ballotKey, fErr)
	}

	yes, _ := ballot.CountVotes()
	k.Logger().Info("admin execute: stuck inbound ballot finalized",
		"utx_id", universalTxKey,
		"ballot_id", ballotKey,
		"yes_votes", yes,
		"voting_threshold", ballot.VotingThreshold,
		"eligible_voters", len(ballot.EligibleVoters),
		"source_chain", inbound.SourceChain,
		"amount", inbound.Amount,
	)

	if execErr := k.finalizeInboundAndExecute(ctx, inbound, universalTxKey); execErr != nil {
		return "", execErr
	}

	return universalTxKey, nil
}

// requireCarriedUnreachablePending accepts only the shape an admin hatch may
// finalize: PENDING, no vote left to cast, YES already at the threshold.
// Threshold is checked explicitly, not inferred — both vote sites hardcode
// SUCCESS today, but this must not depend on that.
func requireCarriedUnreachablePending(ballotKey string, ballot uvalidatortypes.Ballot) error {
	if !ballot.IsUnreachablePending() {
		return fmt.Errorf("ballot %s status is %s; admin execute requires PENDING with every eligible voter already voted (no further vote can be cast). "+
			"A pending ballot that still has an unvoted eligible voter has to be finalized by that voter through the normal vote flow",
			ballotKey, ballot.Status.String())
	}

	if yes, _ := ballot.CountVotes(); int64(yes) < ballot.VotingThreshold {
		return fmt.Errorf("ballot %s has %d YES vote(s) against a voting threshold of %d; admin execute may only finalize a ballot the validators actually carried",
			ballotKey, yes, ballot.VotingThreshold)
	}

	return nil
}
