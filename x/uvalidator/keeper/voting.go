package keeper

import (
	"context"

	errors "cosmossdk.io/errors"

	"github.com/rollchains/pchain/x/uvalidator/types"
)

func (k Keeper) AddVoteToBallot(
	ctx context.Context,
	ballot types.Ballot,
	address string,
	voteResult types.VoteResult,
) (types.Ballot, error) {
	ballot, err := ballot.AddVote(address, voteResult)
	if err != nil {
		return ballot, err
	}
	k.SetBallot(ctx, ballot)
	return ballot, nil
}

func (k Keeper) VoteOnBallot(
	ctx context.Context,
	id string,
	ballotType types.BallotObservationType,
	voter string,
	voteResult types.VoteResult,
	voters []string,
	votesNeeded int64,
	expiryAfterBlocks int64,
) (
	ballot types.Ballot,
	isFinalized bool,
	isNew bool,
	err error) {
	ballot, isNew, err = k.GetOrCreateBallot(ctx, id, ballotType, voters, votesNeeded, expiryAfterBlocks)
	if err != nil {
		return ballot, false, false, errors.Wrap(err, "Error while voting on the ballot")
	}

	if isNew {
		err := k.ActiveBallotIDs.Set(ctx, id)
		if err != nil {
			return ballot, false, false, errors.Wrap(err, "Error while voting on the ballot")
		}
	}

	ballot, err = k.AddVoteToBallot(ctx, ballot, voter, voteResult)
	if err != nil {
		return ballot, false, isNew, err
	}

	ballot, isFinalizing, err := k.CheckIfFinalizingVote(ctx, ballot)
	if err != nil {
		return ballot, false, false, err
	}
	if isFinalizing {
		if err := k.ActiveBallotIDs.Remove(ctx, id); err != nil {
			return ballot, false, isNew, errors.Wrap(err, "failed removing from active ballots")
		}
		if err := k.FinalizedBallotIDs.Set(ctx, id); err != nil {
			return ballot, false, isNew, errors.Wrap(err, "failed adding to finalized ballots")
		}
	}

	return ballot, isFinalized, isNew, nil
}

func (k Keeper) CheckIfFinalizingVote(ctx context.Context, b types.Ballot) (types.Ballot, bool, error) {
	ballot, isFinalizing := b.IsFinalizingVote()
	if !isFinalizing {
		return b, false, nil
	}
	k.SetBallot(ctx, ballot)
	if err := k.SetBallot(ctx, ballot); err != nil {
		return ballot, false, errors.Wrap(err, "failed updating finalized ballot")
	}
	return b, true, nil
}
