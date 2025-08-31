package keeper

import (
	"context"
	"fmt"

	errors "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

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

func (k Keeper) IsBondedUniversalValidator(ctx context.Context, universalValidator string) (bool, error) {
	// Check if the universal validator is in the registered set
	exists, err := k.HasUniversalValidatorInSet(ctx, universalValidator)
	if err != nil {
		return false, fmt.Errorf("failed to check universal validator set: %w", err)
	}
	if !exists {
		return false, nil // not in set → not bonded
	}

	// Get the corresponding core validator
	coreValidatorAddr, found, err := k.GetUniversalToCore(ctx, universalValidator)
	if err != nil {
		return false, fmt.Errorf("failed to get core validator for universal validator %s: %w", universalValidator, err)
	}
	if !found {
		return false, fmt.Errorf("universal validator %s has no mapped core validator", universalValidator)
	}

	coreValAddr, err := sdk.ValAddressFromBech32(coreValidatorAddr)
	if err != nil {
		return false, fmt.Errorf("invalid core validator address: %w", err)
	}

	// Ensure the core validator exists in the staking module
	validator, err := k.stakingKeeper.GetValidator(ctx, coreValAddr)
	if err != nil {
		return false, fmt.Errorf("core validator not found: %w", err)
	}

	// Check that the validator is in bonded status
	if !validator.IsBonded() {
		return false, nil // exists but not bonded
	}

	return true, nil
}

func (k Keeper) IsTombstonedUniversalValidator(ctx context.Context, universalValidator string) (bool, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Check if the universal validator exists in the set
	exists, err := k.HasUniversalValidatorInSet(ctx, universalValidator)
	if err != nil {
		return false, fmt.Errorf("failed to check universal validator set: %w", err)
	}
	if !exists {
		return false, nil // Not in set → cannot be tombstoned
	}

	// Get the corresponding core validator
	coreValidatorAddr, found, err := k.GetUniversalToCore(ctx, universalValidator)
	if err != nil {
		return false, fmt.Errorf("failed to get core validator for universal validator %s: %w", universalValidator, err)
	}
	if !found {
		return false, fmt.Errorf("universal validator %s has no mapped core validator", universalValidator)
	}

	// Convert core validator (operator) address to SDK validator address
	coreValAddr, err := sdk.ValAddressFromBech32(coreValidatorAddr)
	if err != nil {
		return false, fmt.Errorf("failed to get operator address from core validator: %w", err)
	}

	// Query the validator
	validator, err := k.stakingKeeper.GetValidator(sdkCtx, coreValAddr)
	if err != nil {
		return false, fmt.Errorf("core validator not found: %w", err)
	}

	// Get consensus address and check tombstoned status via slashing keeper
	consAddress, err := validator.GetConsAddr()
	if err != nil {
		return false, fmt.Errorf("failed to get consensus address: %w", err)
	}

	return k.slashingKeeper.IsTombstoned(sdkCtx, consAddress), nil
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

	if ballot.Status != types.BallotStatus_BALLOT_STATUS_PENDING {
		return ballot, false, false, errors.Wrap(err, "Error while voting, ballot is already finalized or expired")
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

	return ballot, isFinalizing, isNew, nil
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
