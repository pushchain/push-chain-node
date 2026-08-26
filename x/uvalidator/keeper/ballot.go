package keeper

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/pushchain/push-chain-node/x/uvalidator/types"
)

// MaxExpiriesPerBlock bounds how many ballots a single expiry sweep may
// transition. It keeps the per-block cost of the sweep constant even if a
// large backlog of ballots comes due at once; anything left over stays in
// PendingByExpiry and is picked up by the next block's sweep.
const MaxExpiriesPerBlock = 50

// CreateBallot creates a new ballot with the given parameters, stores it, and marks it as active.
func (k Keeper) CreateBallot(
	ctx context.Context,
	id string,
	ballotType types.BallotObservationType,
	eligibleVoters []string,
	votingThreshold int64,
	expiryAfterBlocks int64,
) (types.Ballot, error) {
	// Get current block height
	blockHeight, err := k.GetBlockHeight(ctx)
	if err != nil {
		return types.Ballot{}, err
	}

	k.Logger().Debug("creating ballot",
		"ballot_id", id,
		"ballot_type", ballotType.String(),
		"eligible_voters", len(eligibleVoters),
		"voting_threshold", votingThreshold,
		"expiry_after_blocks", expiryAfterBlocks,
		"block_height", blockHeight,
	)

	// NOTE: creation deliberately does NOT sweep expired ballots. That scan used
	// to run here on every create and walked the whole active set, paying an
	// IAVL read + unmarshal per entry. Expiry now runs once per block in the
	// module EndBlocker off the PendingByExpiry index instead.

	// Create ballot
	ballot := types.NewBallot(
		id,
		ballotType,
		eligibleVoters,
		votingThreshold,
		blockHeight,
		expiryAfterBlocks,
	)

	// Store the ballot
	if err := k.Ballots.Set(ctx, ballot.Id, ballot); err != nil {
		return types.Ballot{}, err
	}

	// Mark as active
	if err := k.ActiveBallotIDs.Set(ctx, ballot.Id); err != nil {
		return types.Ballot{}, err
	}
	if err := k.indexPending(ctx, ballot.Id, ballot.BlockHeightExpiry); err != nil {
		return types.Ballot{}, err
	}

	k.Logger().Debug("ballot created and marked active",
		"ballot_id", ballot.Id,
		"ballot_type", ballotType.String(),
		"expiry_height", ballot.BlockHeightExpiry,
	)

	return ballot, nil
}

// GetOrCreateBallot returns the ballot if it exists, otherwise creates it.
func (k Keeper) GetOrCreateBallot(
	ctx context.Context,
	id string,
	ballotType types.BallotObservationType,
	voters []string,
	votesNeeded int64,
	expiryAfterBlocks int64,
) (types.Ballot, bool, error) {

	if ballot, err := k.Ballots.Get(ctx, id); err == nil {
		k.Logger().Debug("ballot found (existing)", "ballot_id", id)
		return ballot, false, nil
	}

	k.Logger().Debug("ballot not found, creating new", "ballot_id", id, "ballot_type", ballotType.String())
	newBallot, err := k.CreateBallot(ctx, id, ballotType, voters, votesNeeded, expiryAfterBlocks)

	return newBallot, true, err
}

// GetBallot retrieves a ballot by ID
func (k Keeper) GetBallot(ctx context.Context, id string) (types.Ballot, error) {
	k.Logger().Debug("fetching ballot", "ballot_id", id)
	return k.Ballots.Get(ctx, id)
}

// SetBallot updates an existing ballot
func (k Keeper) SetBallot(ctx context.Context, ballot types.Ballot) error {
	k.Logger().Debug("persisting ballot", "ballot_id", ballot.Id, "ballot_status", ballot.Status.String())
	return k.Ballots.Set(ctx, ballot.Id, ballot)
}

// DeleteBallot removes a ballot and its ID from all collections.
//
// The PendingByExpiry row is keyed by the ballot's expiry height, so the record
// must be read before it is removed. A missing record means there is nothing to
// unindex (DeleteBallot stays idempotent on absent IDs).
func (k Keeper) DeleteBallot(ctx context.Context, id string) error {
	k.Logger().Debug("deleting ballot", "ballot_id", id)
	if ballot, err := k.Ballots.Get(ctx, id); err == nil {
		_ = k.unindexPending(ctx, id, ballot.BlockHeightExpiry)
	}
	if err := k.Ballots.Remove(ctx, id); err != nil {
		return err
	}
	_ = k.ActiveBallotIDs.Remove(ctx, id)
	_ = k.ExpiredBallotIDs.Remove(ctx, id)
	_ = k.FinalizedBallotIDs.Remove(ctx, id)
	return nil
}

// MarkBallotExpired moves a ballot from active to expired.
// Side-effect ordering: secondary indexes are updated before the canonical
// ballot record is rewritten, so the status field is only persisted once the
// active/expired set membership is in its final shape (defensive CEI-style
// ordering; collections.KeySet.Remove is a no-op on absent keys, so retries
// remain safe).
//
// Fires the BallotHooks terminal callback (if registered) AFTER all writes
// have committed. Hook errors are logged but do NOT block the terminal
// transition — the terminal status is the desired outcome regardless of
// secondary-index side-effect failure.
func (k Keeper) MarkBallotExpired(ctx context.Context, id string) error {
	ballot, err := k.Ballots.Get(ctx, id)
	if err != nil {
		return err
	}

	k.Logger().Debug("marking ballot as expired",
		"ballot_id", id,
		"expiry_height", ballot.BlockHeightExpiry,
	)

	if err := k.ActiveBallotIDs.Remove(ctx, id); err != nil {
		return err
	}
	if err := k.unindexPending(ctx, id, ballot.BlockHeightExpiry); err != nil {
		return err
	}
	if err := k.ExpiredBallotIDs.Set(ctx, id); err != nil {
		return err
	}

	ballot.Status = types.BallotStatus_BALLOT_STATUS_EXPIRED
	if err := k.Ballots.Set(ctx, id, ballot); err != nil {
		return err
	}

	k.fireBallotTerminalHook(ctx, ballot.Id, ballot.BallotType, types.BallotStatus_BALLOT_STATUS_EXPIRED)
	return nil
}

// MarkBallotFinalized moves a ballot from active to finalized (PASSED or REJECTED).
// Side-effect ordering matches MarkBallotExpired: secondary indexes are
// updated before the canonical ballot record is rewritten with its final status.
//
// Fires the BallotHooks terminal callback (if registered) AFTER all writes
// have committed. Hook errors are logged but do NOT block the terminal
// transition.
func (k Keeper) MarkBallotFinalized(ctx context.Context, id string, status types.BallotStatus) error {
	if status != types.BallotStatus_BALLOT_STATUS_PASSED && status != types.BallotStatus_BALLOT_STATUS_REJECTED {
		return fmt.Errorf("invalid finalization status: %v", status)
	}

	ballot, err := k.Ballots.Get(ctx, id)
	if err != nil {
		return err
	}

	k.Logger().Debug("marking ballot as finalized",
		"ballot_id", id,
		"final_status", status.String(),
	)

	if err := k.ActiveBallotIDs.Remove(ctx, id); err != nil {
		return err
	}
	if err := k.unindexPending(ctx, id, ballot.BlockHeightExpiry); err != nil {
		return err
	}
	if err := k.FinalizedBallotIDs.Set(ctx, id); err != nil {
		return err
	}

	ballot.Status = status
	if err := k.Ballots.Set(ctx, id, ballot); err != nil {
		return err
	}

	k.fireBallotTerminalHook(ctx, ballot.Id, ballot.BallotType, status)
	return nil
}

// fireBallotTerminalHook invokes the registered BallotHooks (if any) and
// log-swallows any error. Terminal transitions must never be blocked by
// secondary-index side-effect failure.
func (k Keeper) fireBallotTerminalHook(
	ctx context.Context,
	ballotID string,
	ballotType types.BallotObservationType,
	status types.BallotStatus,
) {
	if k.ballotHooks == nil {
		return
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	if err := k.ballotHooks.AfterBallotTerminal(sdkCtx, ballotID, ballotType, status); err != nil {
		k.Logger().Warn("ballot terminal hook returned error",
			"ballot_id", ballotID,
			"ballot_type", ballotType.String(),
			"status", status.String(),
			"err", err.Error(),
		)
	}
}

// GetAdmin returns the Params.Admin address. Used by other modules' admin-gated paths.
func (k Keeper) GetAdmin(ctx context.Context) (string, error) {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return "", err
	}
	return params.Admin, nil
}

// RecomputeBallotQuorum rebuilds a pending ballot's eligible-voter list and
// voting threshold against the current eligible-voter set, preserving votes
// from voters still eligible and dropping votes from voters no longer eligible.
//
// If the recomputed eligible count is zero, the ballot is marked EXPIRED (no
// path to finalization). Otherwise it stays PENDING with the new parameters;
// downstream UVs must re-vote on the same ballot to trigger finalize+execute
// via the normal flow.
//
// Returns the old/new counts and threshold for the response.
func (k Keeper) RecomputeBallotQuorum(ctx context.Context, ballotID string) (
	oldEligibleCount, newEligibleCount, oldThreshold, newThreshold int64,
	newStatus types.BallotStatus,
	err error,
) {
	ballot, err := k.Ballots.Get(ctx, ballotID)
	if err != nil {
		return 0, 0, 0, 0, 0, fmt.Errorf("ballot %s not found: %w", ballotID, err)
	}

	if ballot.Status != types.BallotStatus_BALLOT_STATUS_PENDING {
		return 0, 0, 0, 0, 0, fmt.Errorf("ballot %s is not pending (status=%s); only pending ballots can be recomputed", ballotID, ballot.Status.String())
	}

	oldEligibleCount = int64(len(ballot.EligibleVoters))
	oldThreshold = ballot.VotingThreshold

	// Build the current eligible-voter set in the same valoper-bech32 format
	// the ballot already uses. The voting flow (VoteOnInboundBallot/VoteOnOutboundBallot)
	// passes CoreValidatorAddress strings directly into VoteOnBallot, so the
	// stored EligibleVoters list contains valoper bech32 addresses.
	eligibleUVs, err := k.GetEligibleVoters(ctx)
	if err != nil {
		return 0, 0, 0, 0, 0, fmt.Errorf("failed to fetch eligible voters: %w", err)
	}

	newVoters := make([]string, 0, len(eligibleUVs))
	for _, uv := range eligibleUVs {
		if uv.IdentifyInfo == nil || uv.IdentifyInfo.CoreValidatorAddress == "" {
			k.Logger().Warn("recompute: skipping UV with missing identity info")
			continue
		}
		newVoters = append(newVoters, uv.IdentifyInfo.CoreValidatorAddress)
	}
	newEligibleCount = int64(len(newVoters))

	// Zero eligible voters: no path to finalization. Mark EXPIRED.
	if newEligibleCount == 0 {
		if err := k.MarkBallotExpired(ctx, ballotID); err != nil {
			return 0, 0, 0, 0, 0, fmt.Errorf("failed to mark ballot expired on zero-eligible recompute: %w", err)
		}
		k.Logger().Info("ballot recompute: zero eligible voters → marked expired",
			"ballot_id", ballotID,
			"old_eligible", oldEligibleCount,
		)
		return oldEligibleCount, 0, oldThreshold, 0, types.BallotStatus_BALLOT_STATUS_EXPIRED, nil
	}

	// Compute new threshold using the same formula uexecutor's voting flow uses.
	// We use 2/3 + 1 — matches `(VotesThresholdNumerator * N) / VotesThresholdDenominator + 1`.
	newThreshold = (2*newEligibleCount)/3 + 1

	// Preserve votes from voters still in the new list; new voters → NOT_YET.
	oldVotes := make(map[string]types.VoteResult, len(ballot.EligibleVoters))
	for i, voter := range ballot.EligibleVoters {
		if i < len(ballot.Votes) {
			oldVotes[voter] = ballot.Votes[i]
		}
	}
	newVotesArr := make([]types.VoteResult, len(newVoters))
	for i, voter := range newVoters {
		if prev, ok := oldVotes[voter]; ok {
			newVotesArr[i] = prev
		} else {
			newVotesArr[i] = types.VoteResult_VOTE_RESULT_NOT_YET_VOTED
		}
	}

	ballot.EligibleVoters = newVoters
	ballot.Votes = newVotesArr
	ballot.VotingThreshold = newThreshold

	if err := k.Ballots.Set(ctx, ballotID, ballot); err != nil {
		return 0, 0, 0, 0, 0, fmt.Errorf("failed to persist recomputed ballot: %w", err)
	}

	k.Logger().Info("ballot recomputed",
		"ballot_id", ballotID,
		"old_eligible", oldEligibleCount,
		"new_eligible", newEligibleCount,
		"old_threshold", oldThreshold,
		"new_threshold", newThreshold,
	)

	return oldEligibleCount, newEligibleCount, oldThreshold, newThreshold, types.BallotStatus_BALLOT_STATUS_PENDING, nil
}

// indexPending adds the (expiryHeight, ballotID) row that mirrors an entry in
// ActiveBallotIDs. Every ActiveBallotIDs.Set must be paired with this call, or
// the ballot becomes invisible to the expiry sweep.
func (k Keeper) indexPending(ctx context.Context, id string, expiryHeight int64) error {
	return k.PendingByExpiry.Set(ctx, collections.Join(expiryHeight, id))
}

// unindexPending drops the (expiryHeight, ballotID) row. Every
// ActiveBallotIDs.Remove must be paired with this call, or the sweep keeps
// re-visiting a ballot that is no longer active.
func (k Keeper) unindexPending(ctx context.Context, id string, expiryHeight int64) error {
	return k.PendingByExpiry.Remove(ctx, collections.Join(expiryHeight, id))
}

// ExpireBallotsBeforeHeight marks every active ballot whose expiry height is at
// or below currentHeight as expired, up to MaxExpiriesPerBlock per call.
//
// It ranges over the PendingByExpiry index rather than ActiveBallotIDs. Because
// collections.Pair orders by its first component, iteration ends at the first
// row past currentHeight: ballots that are not yet due are never visited, and
// no Ballots.Get is needed to decide whether a ballot is due — the expiry
// height IS the key. Only ballots actually being expired are ever loaded.
//
// It keeps the original two-phase shape: IDs are collected while the iterator
// is open and mutated only after it is closed. Mutating a collection mid-
// iteration skips entries at best and panics at worst.
func (k Keeper) ExpireBallotsBeforeHeight(ctx context.Context, currentHeight int64) error {
	// NewPrefixUntilPairRange ends at the prefix-end of currentHeight, so the
	// range is inclusive of currentHeight — matching the `<=` expiry semantics.
	rng := collections.NewPrefixUntilPairRange[int64, string](currentHeight)

	iter, err := k.PendingByExpiry.Iterate(ctx, rng)
	if err != nil {
		return err
	}

	// Phase 1: collect the due index keys, bounded by MaxExpiriesPerBlock.
	var due []collections.Pair[int64, string]
	for ; iter.Valid(); iter.Next() {
		key, err := iter.Key()
		if err != nil {
			iter.Close()
			return err
		}

		due = append(due, key)
		if len(due) >= MaxExpiriesPerBlock {
			break
		}
	}

	// Close iterator explicitly before mutation phase to release the IAVL snapshot
	iter.Close()

	if len(due) == 0 {
		return nil
	}

	k.Logger().Debug("expiring stale ballots", "count", len(due), "current_height", currentHeight)

	// Phase 2: expire collected ballots (safe — iterator is closed)
	for _, key := range due {
		id := key.K2()
		err := k.MarkBallotExpired(ctx, id)
		switch {
		case err == nil:
		case errors.Is(err, collections.ErrNotFound):
			// Defensive: an index row whose ballot record no longer exists must
			// not wedge the sweep every block. Drop the orphan row and continue.
			k.Logger().Warn("dropping orphaned ballot expiry index entry",
				"ballot_id", id,
				"expiry_height", key.K1(),
			)
			if rErr := k.PendingByExpiry.Remove(ctx, key); rErr != nil {
				return rErr
			}
		default:
			return err
		}
	}

	return nil
}
