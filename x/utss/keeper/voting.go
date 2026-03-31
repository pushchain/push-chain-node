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

	currentHeight := sdkCtx.BlockHeight()

	// ensure process hasn't expired
	if currentHeight >= existing.ExpiryHeight {
		return false, false, fmt.Errorf("process expired at height %d (current %d)", existing.ExpiryHeight, currentHeight)
	}

	// compute delta = number of blocks from now until expiry
	expiryAfterBlocks := existing.ExpiryHeight - currentHeight
	if expiryAfterBlocks <= 0 {
		return false, false, fmt.Errorf("invalid expiry delta: %d", expiryAfterBlocks)
	}

	// votesNeeded = number of participants in the tss process
	// 100% quorum needed
	votesNeeded := int64(len(existing.Participants))
	if votesNeeded <= 0 {
		return false, false, fmt.Errorf("no participants in process %d", processId)
	}

	k.Logger().Debug("casting tss ballot vote",
		"process_id", processId,
		"key_id", keyId,
		"validator", universalValidator.String(),
		"votes_needed", votesNeeded,
		"expiry_after_blocks", expiryAfterBlocks,
	)

	// Step 2: Call VoteOnBallot for tss
	_, isFinalized, isNew, err = k.uvalidatorKeeper.VoteOnBallot(
		ctx,
		ballotKey,
		uvalidatortypes.BallotObservationType_BALLOT_OBSERVATION_TYPE_TSS_KEY,
		universalValidator.String(),
		uvalidatortypes.VoteResult_VOTE_RESULT_SUCCESS,
		existing.Participants,
		int64(votesNeeded),
		expiryAfterBlocks,
	)
	if err != nil {
		return false, false, err
	}

	if isNew {
		k.Logger().Debug("new tss ballot created",
			"process_id", processId,
			"key_id", keyId,
		)
	}
	if isFinalized {
		k.Logger().Debug("tss ballot finalized",
			"process_id", processId,
			"key_id", keyId,
		)
	}

	return isFinalized, isNew, nil
}

const (
	// FundMigration uses 2/3 quorum like outbound observations
	fundMigrationVotesNumerator   = 2
	fundMigrationVotesDenominator = 3
	fundMigrationExpiryBlocks     = 100_000_000
)

func (k Keeper) VoteOnFundMigrationBallot(
	ctx context.Context,
	universalValidator sdk.ValAddress,
	migrationId uint64,
	txHash string,
	success bool,
) (isFinalized bool, isNew bool, err error) {

	ballotKey := types.GetFundMigrationBallotKey(migrationId, txHash, success)

	universalValidatorSet, err := k.uvalidatorKeeper.GetEligibleVoters(ctx)
	if err != nil {
		return false, false, err
	}

	totalValidators := len(universalValidatorSet)
	votesNeeded := (fundMigrationVotesNumerator*totalValidators)/fundMigrationVotesDenominator + 1

	validatorStrs := make([]string, len(universalValidatorSet))
	for i, v := range universalValidatorSet {
		validatorStrs[i] = v.IdentifyInfo.CoreValidatorAddress
	}

	voteResult := uvalidatortypes.VoteResult_VOTE_RESULT_SUCCESS
	if !success {
		voteResult = uvalidatortypes.VoteResult_VOTE_RESULT_FAILURE
	}

	k.Logger().Debug("voting on fund migration ballot",
		"ballot_key", ballotKey,
		"validator", universalValidator.String(),
		"migration_id", migrationId,
		"total_validators", totalValidators,
		"votes_needed", votesNeeded,
	)

	_, isFinalized, isNew, err = k.uvalidatorKeeper.VoteOnBallot(
		ctx,
		ballotKey,
		uvalidatortypes.BallotObservationType_BALLOT_OBSERVATION_TYPE_FUND_MIGRATION,
		universalValidator.String(),
		voteResult,
		validatorStrs,
		int64(votesNeeded),
		int64(fundMigrationExpiryBlocks),
	)
	if err != nil {
		return false, false, err
	}

	if isNew {
		k.Logger().Debug("fund migration ballot created", "ballot_key", ballotKey, "migration_id", migrationId)
	}
	if isFinalized {
		k.Logger().Info("fund migration ballot finalized", "ballot_key", ballotKey, "migration_id", migrationId)
	}

	return isFinalized, isNew, nil
}
