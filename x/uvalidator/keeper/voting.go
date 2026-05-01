package keeper

import (
	"context"
	"fmt"

	errors "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/pushchain/push-chain-node/x/uvalidator/types"
)

func (k Keeper) AddVoteToBallot(
	ctx context.Context,
	ballot types.Ballot,
	address string,
	voteResult types.VoteResult,
) (types.Ballot, error) {
	k.Logger().Debug("adding vote to ballot",
		"ballot_id", ballot.Id,
		"voter", address,
		"vote_result", voteResult.String(),
	)

	ballot, err := ballot.AddVote(address, voteResult)
	if err != nil {
		return ballot, err
	}
	if err := k.SetBallot(ctx, ballot); err != nil {
		return ballot, errors.Wrap(err, "failed to persist ballot after adding vote")
	}

	k.Logger().Debug("vote added to ballot",
		"ballot_id", ballot.Id,
		"voter", address,
		"vote_result", voteResult.String(),
	)

	return ballot, nil
}

func (k Keeper) IsBondedUniversalValidator(ctx context.Context, universalValidator string) (bool, error) {
	k.Logger().Debug("checking bonded status of universal validator", "validator", universalValidator)

	accAddr, err := sdk.AccAddressFromBech32(universalValidator)
	if err != nil {
		return false, fmt.Errorf("invalid signer address: %w", err)
	}

	valAddr := sdk.ValAddress(accAddr)

	// Check if the universal validator is in the registered set
	exists, err := k.HasUniversalValidatorInSet(ctx, valAddr)
	if err != nil {
		return false, fmt.Errorf("failed to check universal validator set: %w", err)
	}
	if !exists {
		k.Logger().Debug("validator not found in universal validator set", "validator", universalValidator)
		return false, fmt.Errorf("validator %s not present in the registered universal validators set", valAddr.String())
	}

	// Ensure the universal validator exists in the staking module
	validator, err := k.StakingKeeper.GetValidator(ctx, valAddr)
	if err != nil {
		return false, fmt.Errorf("core validator not found: %w", err)
	}

	// Check that the validator is in bonded status
	if !validator.IsBonded() {
		k.Logger().Debug("universal validator is not bonded", "validator", universalValidator)
		return false, nil // exists but not bonded
	}

	k.Logger().Debug("universal validator is bonded", "validator", universalValidator)
	return true, nil
}

func (k Keeper) IsTombstonedUniversalValidator(ctx context.Context, universalValidator string) (bool, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	k.Logger().Debug("checking tombstoned status of universal validator", "validator", universalValidator)

	accAddr, err := sdk.AccAddressFromBech32(universalValidator)
	if err != nil {
		return false, fmt.Errorf("invalid signer address: %w", err)
	}

	valAddr := sdk.ValAddress(accAddr)

	// Check if the universal validator exists in the set
	exists, err := k.HasUniversalValidatorInSet(ctx, valAddr)
	if err != nil {
		return false, fmt.Errorf("failed to check universal validator set: %w", err)
	}
	if !exists {
		k.Logger().Debug("validator not found in universal validator set", "validator", universalValidator)
		return false, fmt.Errorf("validator %s not present in the registered universal validators set", valAddr.String())
	}

	// Query the validator
	validator, err := k.StakingKeeper.GetValidator(sdkCtx, valAddr)
	if err != nil {
		return false, fmt.Errorf("core validator not found: %w", err)
	}

	// Get consensus address and check tombstoned status via slashing keeper
	consAddress, err := validator.GetConsAddr()
	if err != nil {
		return false, fmt.Errorf("failed to get consensus address: %w", err)
	}

	isTombstoned := k.SlashingKeeper.IsTombstoned(sdkCtx, consAddress)
	k.Logger().Debug("universal validator tombstone status",
		"validator", universalValidator,
		"is_tombstoned", isTombstoned,
	)
	return isTombstoned, nil
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

	k.Logger().Debug("vote on ballot",
		"ballot_id", id,
		"ballot_type", ballotType.String(),
		"voter", voter,
		"vote_result", voteResult.String(),
		"votes_needed", votesNeeded,
	)

	ballot, isNew, err = k.GetOrCreateBallot(ctx, id, ballotType, voters, votesNeeded, expiryAfterBlocks)
	if err != nil {
		return ballot, false, false, errors.Wrap(err, "Error while voting on the ballot")
	}

	if ballot.Status != types.BallotStatus_BALLOT_STATUS_PENDING {
		k.Logger().Warn("ballot is not in pending state, cannot vote",
			"ballot_id", id,
			"ballot_status", ballot.Status.String(),
			"voter", voter,
		)
		return ballot, false, false, fmt.Errorf("ballot %s is already %s", id, ballot.Status.String())
	}

	if isNew {
		k.Logger().Debug("created new ballot", "ballot_id", id, "ballot_type", ballotType.String())
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

	return ballot, isFinalizing, isNew, nil
}

// CheckIfFinalizingVote inspects whether the just-cast vote pushes the ballot
// over its threshold and, if so, drives the finalization through
// MarkBallotFinalized — the single canonical write path for terminal status
// transitions, which applies CEI-style ordering on the secondary indexes.
func (k Keeper) CheckIfFinalizingVote(ctx context.Context, b types.Ballot) (types.Ballot, bool, error) {
	ballot, isFinalizing := b.IsFinalizingVote()
	if !isFinalizing {
		return b, false, nil
	}

	k.Logger().Debug("ballot reached finalization threshold",
		"ballot_id", ballot.Id,
		"ballot_status", ballot.Status.String(),
	)

	if err := k.MarkBallotFinalized(ctx, ballot.Id, ballot.Status); err != nil {
		return ballot, false, errors.Wrap(err, "failed updating finalized ballot")
	}

	k.Logger().Debug("ballot finalized",
		"ballot_id", ballot.Id,
		"ballot_status", ballot.Status.String(),
	)
	return ballot, true, nil
}
