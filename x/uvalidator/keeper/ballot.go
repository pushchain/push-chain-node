package keeper

import (
	"context"
	"fmt"

	"github.com/pushchain/push-chain-node/x/uvalidator/types"
)

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

	// First, expire any old ballots before this height
	if err := k.ExpireBallotsBeforeHeight(ctx, blockHeight); err != nil {
		return types.Ballot{}, err
	}

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

// DeleteBallot removes a ballot and its ID from all collections
func (k Keeper) DeleteBallot(ctx context.Context, id string) error {
	k.Logger().Debug("deleting ballot", "ballot_id", id)
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
	if err := k.ExpiredBallotIDs.Set(ctx, id); err != nil {
		return err
	}

	ballot.Status = types.BallotStatus_BALLOT_STATUS_EXPIRED
	return k.Ballots.Set(ctx, id, ballot)
}

// MarkBallotFinalized moves a ballot from active to finalized (PASSED or REJECTED).
// Side-effect ordering matches MarkBallotExpired: secondary indexes are
// updated before the canonical ballot record is rewritten with its final status.
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
	if err := k.FinalizedBallotIDs.Set(ctx, id); err != nil {
		return err
	}

	ballot.Status = status
	return k.Ballots.Set(ctx, id, ballot)
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

// ExpireBallotsBeforeHeight checks active ballots and marks expired ones.
// It uses a two-phase approach: first collect IDs to expire, then mutate,
// to avoid modifying the ActiveBallotIDs collection during iteration.
func (k Keeper) ExpireBallotsBeforeHeight(ctx context.Context, currentHeight int64) error {
	iter, err := k.ActiveBallotIDs.Iterate(ctx, nil)
	if err != nil {
		return err
	}

	// Phase 1: collect IDs to expire
	var toExpire []string
	for ; iter.Valid(); iter.Next() {
		id, err := iter.Key()
		if err != nil {
			iter.Close()
			return err
		}

		ballot, err := k.Ballots.Get(ctx, id)
		if err != nil {
			iter.Close()
			return err
		}

		if ballot.BlockHeightExpiry <= currentHeight {
			toExpire = append(toExpire, id)
		}
	}

	// Close iterator explicitly before mutation phase to release the IAVL snapshot
	iter.Close()

	if len(toExpire) > 0 {
		k.Logger().Debug("expiring stale ballots", "count", len(toExpire), "current_height", currentHeight)
	}

	// Phase 2: expire collected ballots (safe — iterator is closed)
	for _, id := range toExpire {
		if err := k.MarkBallotExpired(ctx, id); err != nil {
			return err
		}
	}

	return nil
}
