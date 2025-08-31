package keeper

import (
	"context"
	"fmt"

	"github.com/rollchains/pchain/x/ue/types"
	uvalidatortypes "github.com/rollchains/pchain/x/uvalidator/types"
)

func (k Keeper) VoteOnInboundBallot(
	ctx context.Context,
	universalValidator string,
	inbound types.Inbound,
) (isFinalized bool,
	isNew bool,
	err error) {
	// Step 1: Check if the inbound is enabled
	chainEnabled, err := k.uregistryKeeper.IsChainInboundEnabled(ctx, inbound.SourceChain)
	if err != nil {
		return false, false, err
	}
	if !chainEnabled {
		return false, false, fmt.Errorf("inbound tx is not enabled")
	}

	ballotKey := types.GetInboundKey(inbound)

	universalValidatorSet, err := k.uvalidatorKeeper.GetUniversalValidatorSet(ctx)
	if err != nil {
		return false, false, err
	}

	// number of validators
	totalValidators := len(universalValidatorSet)

	// votesNeeded = ceil(2/3 * totalValidators)
	// >2/3 quorum similar to tendermint
	votesNeeded := (totalValidators*types.VotesThresholdNumerator + types.VotesThresholdDenominator - 1) / types.VotesThresholdDenominator

	// Step 2: Call VoteOnBallot for this inbound synthetic
	_, isFinalized, isNew, err = k.uvalidatorKeeper.VoteOnBallot(
		ctx,
		ballotKey,
		uvalidatortypes.BallotObservationType_BALLOT_OBSERVATION_TYPE_INBOUND_TX,
		universalValidator,
		uvalidatortypes.VoteResult_VOTE_RESULT_SUCCESS,
		universalValidatorSet,
		int64(votesNeeded),
		int64(types.DefaultExpiryAfterBlocks),
	)
	if err != nil {
		return false, false, err
	}

	return isFinalized, isNew, nil
}
