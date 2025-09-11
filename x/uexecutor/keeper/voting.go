package keeper

import (
	"context"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/pushchain/push-chain-node/x/uexecutor/types"
	uvalidatortypes "github.com/pushchain/push-chain-node/x/uvalidator/types"
)

func (k Keeper) VoteOnInboundBallot(
	ctx context.Context,
	universalValidator sdk.ValAddress,
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

	// Convert []sdk.ValAddress → []string
	universalValidatorSetStrs := make([]string, len(universalValidatorSet))
	for i, v := range universalValidatorSet {
		universalValidatorSetStrs[i] = v.String()
	}

	// Step 2: Call VoteOnBallot for this inbound synthetic
	_, isFinalized, isNew, err = k.uvalidatorKeeper.VoteOnBallot(
		ctx,
		ballotKey,
		uvalidatortypes.BallotObservationType_BALLOT_OBSERVATION_TYPE_INBOUND_TX,
		universalValidator.String(),
		uvalidatortypes.VoteResult_VOTE_RESULT_SUCCESS,
		universalValidatorSetStrs,
		int64(votesNeeded),
		int64(types.DefaultExpiryAfterBlocks),
	)
	if err != nil {
		return false, false, err
	}

	return isFinalized, isNew, nil
}
