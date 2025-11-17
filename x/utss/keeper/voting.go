package keeper

import (
	"context"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/pushchain/push-chain-node/x/utss/types"
	uvalidatortypes "github.com/pushchain/push-chain-node/x/uvalidator/types"
)

func (k Keeper) VoteOnTssBallot(
	ctx context.Context,
	universalValidator sdk.ValAddress,
	processId uint64,
	tssPubKey, keyId string,
) (isFinalized bool,
	isNew bool,
	err error) {

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	ballotKey := types.GetTssBallotKey(processId, tssPubKey, keyId)

	// Check if a current process exists and is still active (not expired and pending)
	existing, err := k.CurrentTssProcess.Get(ctx)
	if err != nil {
		return false, false, fmt.Errorf("no active TSS process")
	}

	if existing.Id != processId {
		return false, false, fmt.Errorf(
			"invalid vote: active process is %d, got %d",
			existing.Id, processId,
		)
	}

	if sdkCtx.BlockHeight() >= existing.ExpiryHeight {
		return false, false, fmt.Errorf("process expired")
	}

	expiryHeight := existing.ExpiryHeight

	// votesNeeded = number of participants in the tss process
	// 100% quorum needed
	votesNeeded := len(existing.Participants)

	// Step 2: Call VoteOnBallot for tss
	_, isFinalized, isNew, err = k.uvalidatorKeeper.VoteOnBallot(
		ctx,
		ballotKey,
		uvalidatortypes.BallotObservationType_BALLOT_OBSERVATION_TYPE_TSS_KEY,
		universalValidator.String(),
		uvalidatortypes.VoteResult_VOTE_RESULT_SUCCESS,
		existing.Participants,
		int64(votesNeeded),
		expiryHeight,
	)
	if err != nil {
		return false, false, err
	}

	return isFinalized, isNew, nil
}
