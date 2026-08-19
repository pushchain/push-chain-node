package keeper

import (
	"context"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/pushchain/push-chain-node/x/ucallback/types"
	uvalidatortypes "github.com/pushchain/push-chain-node/x/uvalidator/types"
)

// VoteOnReadBallot casts one validator's vote on the ballot for (requestID, result)
// and reports whether that vote carried it to quorum.
//
// Mirrors x/uexecutor's VoteOnOutboundBallot for the threshold and voter set, but
// not for expiry: the ballot is given the request's own deadline rather than
// uexecutor's inert 100M blocks, so the two cannot disagree about when the request
// is over.
func (k Keeper) VoteOnReadBallot(
	ctx context.Context,
	universalValidator sdk.ValAddress,
	requestID string,
	result *types.ReadResult,
	expiryHeight uint64,
) (ballotKey string, isFinalized bool, isNew bool, err error) {
	ballotKey, err = types.GetReadBallotKey(requestID, result)
	if err != nil {
		return "", false, false, err
	}

	voters, err := k.uvalidatorKeeper.GetEligibleVoters(ctx)
	if err != nil {
		return "", false, false, err
	}
	if len(voters) == 0 {
		return "", false, false, fmt.Errorf("no eligible universal validators")
	}

	// votesNeeded = floor(2/3 * n) + 1, i.e. a strict >2/3 majority, matching
	// tendermint and every other ballot on this chain.
	votesNeeded := (types.VotesThresholdNumerator*len(voters))/types.VotesThresholdDenominator + 1

	voterAddrs := make([]string, len(voters))
	for i, v := range voters {
		voterAddrs[i] = v.IdentifyInfo.CoreValidatorAddress
	}

	expiryAfterBlocks := types.BallotExpiryAfterBlocks(
		expiryHeight, sdk.UnwrapSDKContext(ctx).BlockHeight())

	k.Logger().Debug("voting on read ballot",
		"ballot_key", ballotKey,
		"request_id", requestID,
		"validator", universalValidator.String(),
		"total_validators", len(voters),
		"votes_needed", votesNeeded,
		"expiry_height", expiryHeight,
		"expiry_after_blocks", expiryAfterBlocks,
	)

	_, isFinalized, isNew, err = k.uvalidatorKeeper.VoteOnBallot(
		ctx,
		ballotKey,
		uvalidatortypes.BallotObservationType_BALLOT_OBSERVATION_TYPE_READ_RESULT,
		universalValidator.String(),
		// Always SUCCESS: disagreement is expressed by landing on a different
		// ballot key, not by voting FAILURE on a shared one. A FAILURE vote here
		// would mean "this exact observation is wrong", which no validator is in a
		// position to assert — it only knows what it observed itself.
		uvalidatortypes.VoteResult_VOTE_RESULT_SUCCESS,
		voterAddrs,
		int64(votesNeeded),
		expiryAfterBlocks,
	)
	if err != nil {
		return "", false, false, err
	}

	if isNew {
		k.Logger().Debug("read ballot created", "ballot_key", ballotKey, "request_id", requestID)
	}
	if isFinalized {
		k.Logger().Info("read ballot finalized", "ballot_key", ballotKey, "request_id", requestID)
	}

	return ballotKey, isFinalized, isNew, nil
}

// VoteReadResult records a universal validator's observation of a read request.
//
// Reaching quorum here does NOT fulfil the request — it only settles what was
// observed. The fulfilment EVM call is driven by the ballot terminal hook (C7), so
// that it runs exactly once no matter which validator's vote happened to be the
// deciding one.
func (k Keeper) VoteReadResult(
	ctx context.Context,
	universalValidator sdk.ValAddress,
	requestID string,
	result *types.ReadResult,
) (bool, error) {
	if err := types.ValidateReadResult(result); err != nil {
		return false, err
	}

	ur, found := k.GetUniversalRead(ctx, requestID)
	if !found {
		return false, fmt.Errorf("read request not found: %s", requestID)
	}

	// Only unsettled requests accept votes. Without this a validator could keep
	// voting on a request that already fulfilled, creating ballots that the
	// terminal hook would then try to act on a second time.
	if isSettled(ur.Status) {
		return false, fmt.Errorf("read request %s is already %s", requestID, ur.Status)
	}

	if ur.Request == nil {
		return false, fmt.Errorf("read request %s has no request body", requestID)
	}

	// Reject votes on a request whose deadline has passed. AllPendingReadRequests
	// already withholds these, so an honest validator will not be voting on one —
	// but the query is a convenience, not the enforcement point.
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	if ur.Request.ExpiryBlockHeight <= uint64(sdkCtx.BlockHeight()) {
		return false, fmt.Errorf("read request %s expired at height %d",
			requestID, ur.Request.ExpiryBlockHeight)
	}

	// Cache the vote so a failure partway through leaves no half-written ballot.
	tmpCtx, commit := sdkCtx.CacheContext()

	ballotKey, err := types.GetReadBallotKey(requestID, result)
	if err != nil {
		return false, err
	}

	// The record must carry this ballot key and observation BEFORE the vote is
	// cast, not after.
	//
	// VoteOnBallot fires the terminal hook synchronously the moment this vote
	// reaches quorum, and that hook finds the request BY ballot key and needs
	// ur.Result to fulfil it. Writing them afterwards left the deciding vote
	// looking at a record still pointing at whatever observation was voted
	// previously — so the hook missed, and a read that had genuinely reached
	// quorum sat in VOTING until the sweeper expired and refunded it. It also put
	// this write after the hook's, clobbering FULFILLED back to VOTING.
	//
	// Until a ballot finalizes these two fields track whichever observation this
	// validator's vote most recently landed on; quorum is what makes them binding.
	ur.Status = types.UniversalReadStatus_UNIVERSAL_READ_STATUS_VOTING
	ur.BallotKey = ballotKey
	ur.Result = result
	if err := k.SetUniversalRead(tmpCtx, ur); err != nil {
		return false, err
	}

	_, isFinalized, _, err := k.VoteOnReadBallot(
		tmpCtx, universalValidator, requestID, result, ur.Request.ExpiryBlockHeight)
	if err != nil {
		return false, err
	}

	if !isFinalized {
		// The observation was staged above only so the terminal hook could reach it.
		// No ballot passed, so nothing is settled and the record must not imply a
		// consensus that does not exist — a stored Result means quorum.
		//
		// Re-read rather than reusing `ur`: the vote may have driven some OTHER
		// ballot terminal, and writing our stale copy back would undo that.
		cur, found := k.GetUniversalRead(tmpCtx, requestID)
		if found && !isSettled(cur.Status) {
			cur.Result = nil
			if err := k.SetUniversalRead(tmpCtx, cur); err != nil {
				return false, err
			}
		}
	}

	commit()

	k.Logger().Info("read result vote recorded",
		"request_id", requestID,
		"validator", universalValidator.String(),
		"ballot_key", ballotKey,
		"finalized", isFinalized,
	)

	return isFinalized, nil
}
