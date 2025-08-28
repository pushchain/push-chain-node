package keeper

import (
	"context"
	"fmt"

	"github.com/rollchains/pchain/x/uvalidator/types"
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
		return ballot, false, nil
	}

	newBallot, err := k.CreateBallot(ctx, id, ballotType, voters, votesNeeded, expiryAfterBlocks)

	return newBallot, true, err
}

// GetBallot retrieves a ballot by ID
func (k Keeper) GetBallot(ctx context.Context, id string) (types.Ballot, error) {
	return k.Ballots.Get(ctx, id)
}

// SetBallot updates an existing ballot
func (k Keeper) SetBallot(ctx context.Context, ballot types.Ballot) error {
	return k.Ballots.Set(ctx, ballot.Id, ballot)
}

// DeleteBallot removes a ballot and its ID from all collections
func (k Keeper) DeleteBallot(ctx context.Context, id string) error {
	if err := k.Ballots.Remove(ctx, id); err != nil {
		return err
	}
	_ = k.ActiveBallotIDs.Remove(ctx, id)
	_ = k.ExpiredBallotIDs.Remove(ctx, id)
	_ = k.FinalizedBallotIDs.Remove(ctx, id)
	return nil
}

// MarkBallotExpired moves a ballot from active to expired
func (k Keeper) MarkBallotExpired(ctx context.Context, id string) error {
	ballot, err := k.Ballots.Get(ctx, id)
	if err != nil {
		return err
	}

	ballot.Status = types.BallotStatus_BALLOT_STATUS_EXPIRED
	if err := k.Ballots.Set(ctx, id, ballot); err != nil {
		return err
	}

	if err := k.ActiveBallotIDs.Remove(ctx, id); err != nil {
		return err
	}
	return k.ExpiredBallotIDs.Set(ctx, id)
}

// MarkBallotFinalized moves a ballot from active to finalized (PASSED or REJECTED)
func (k Keeper) MarkBallotFinalized(ctx context.Context, id string, status types.BallotStatus) error {
	if status != types.BallotStatus_BALLOT_STATUS_PASSED && status != types.BallotStatus_BALLOT_STATUS_REJECTED {
		return fmt.Errorf("invalid finalization status: %v", status)
	}

	ballot, err := k.Ballots.Get(ctx, id)
	if err != nil {
		return err
	}

	ballot.Status = status
	if err := k.Ballots.Set(ctx, id, ballot); err != nil {
		return err
	}

	if err := k.ActiveBallotIDs.Remove(ctx, id); err != nil {
		return err
	}
	return k.FinalizedBallotIDs.Set(ctx, id)
}

// ExpireBallotsBeforeHeight checks active ballots and marks expired ones
func (k Keeper) ExpireBallotsBeforeHeight(ctx context.Context, currentHeight int64) error {
	iter, err := k.ActiveBallotIDs.Iterate(ctx, nil)
	if err != nil {
		return err
	}
	defer iter.Close()

	for ; iter.Valid(); iter.Next() {
		id, err := iter.Key()
		if err != nil {
			return err
		}

		ballot, err := k.Ballots.Get(ctx, id)
		if err != nil {
			return err
		}

		if ballot.BlockHeightExpiry < currentHeight {
			if err := k.MarkBallotExpired(ctx, id); err != nil {
				return err
			}
		}
	}
	return nil
}
