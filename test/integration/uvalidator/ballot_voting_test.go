package integrationtest

import (
	"fmt"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/app"
	utils "github.com/pushchain/push-chain-node/test/utils"
	uvalidatortypes "github.com/pushchain/push-chain-node/x/uvalidator/types"
)

// setupBallotTest creates a chain app with the given number of validators.
func setupBallotTest(t *testing.T, numVals int) (*app.ChainApp, sdk.Context, []stakingtypes.Validator) {
	t.Helper()
	chainApp, ctx, _, validators := utils.SetAppWithMultipleValidators(t, numVals)
	return chainApp, ctx, validators
}

// voterAddrs converts staking validators' operator addresses into acc-address bech32 strings,
// which is the format expected by the ballot system.
func voterAddrs(t *testing.T, validators []stakingtypes.Validator) []string {
	t.Helper()
	addrs := make([]string, len(validators))
	for i, val := range validators {
		valAddr, err := sdk.ValAddressFromBech32(val.OperatorAddress)
		require.NoError(t, err)
		addrs[i] = sdk.AccAddress(valAddr).String()
	}
	return addrs
}

// registerUniversalValidator registers a staking validator as an active universal validator.
func registerUniversalValidator(t *testing.T, chainApp *app.ChainApp, ctx sdk.Context, val stakingtypes.Validator) {
	t.Helper()
	valAddr, err := sdk.ValAddressFromBech32(val.OperatorAddress)
	require.NoError(t, err)
	uv := uvalidatortypes.UniversalValidator{
		IdentifyInfo: &uvalidatortypes.IdentityInfo{CoreValidatorAddress: val.OperatorAddress},
		LifecycleInfo: &uvalidatortypes.LifecycleInfo{
			CurrentStatus: uvalidatortypes.UVStatus_UV_STATUS_ACTIVE,
		},
		NetworkInfo: &uvalidatortypes.NetworkInfo{PeerId: "test", MultiAddrs: []string{"test"}},
	}
	require.NoError(t, chainApp.UvalidatorKeeper.UniversalValidatorSet.Set(ctx, valAddr, uv))
}

// ─── CreateBallot ────────────────────────────────────────────────────────────

func TestIntegration_CreateBallot(t *testing.T) {
	t.Run("creates ballot and marks it active", func(t *testing.T) {
		chainApp, ctx, validators := setupBallotTest(t, 3)
		k := chainApp.UvalidatorKeeper
		voters := voterAddrs(t, validators)

		ballot, err := k.CreateBallot(ctx, "ballot-create-1",
			uvalidatortypes.BallotObservationType_BALLOT_OBSERVATION_TYPE_INBOUND_TX,
			voters, 2, 100)
		require.NoError(t, err)

		// Verify fields
		require.Equal(t, "ballot-create-1", ballot.Id)
		require.Equal(t, uvalidatortypes.BallotStatus_BALLOT_STATUS_PENDING, ballot.Status)
		require.Equal(t, int64(2), ballot.VotingThreshold)
		require.Equal(t, voters, ballot.EligibleVoters)
		require.Len(t, ballot.Votes, len(voters))

		// Verify stored in Ballots map
		stored, err := k.GetBallot(ctx, "ballot-create-1")
		require.NoError(t, err)
		require.Equal(t, ballot.Id, stored.Id)

		// Verify marked active
		isActive, err := k.ActiveBallotIDs.Has(ctx, "ballot-create-1")
		require.NoError(t, err)
		require.True(t, isActive, "ballot should be in ActiveBallotIDs")

		// Verify NOT in expired or finalized
		isExpired, err := k.ExpiredBallotIDs.Has(ctx, "ballot-create-1")
		require.NoError(t, err)
		require.False(t, isExpired)

		isFinalized, err := k.FinalizedBallotIDs.Has(ctx, "ballot-create-1")
		require.NoError(t, err)
		require.False(t, isFinalized)
	})

	t.Run("expiry height is correctly computed", func(t *testing.T) {
		chainApp, ctx, validators := setupBallotTest(t, 2)
		k := chainApp.UvalidatorKeeper
		voters := voterAddrs(t, validators)

		ctx = ctx.WithBlockHeight(10)
		ballot, err := k.CreateBallot(ctx, "ballot-expiry",
			uvalidatortypes.BallotObservationType_BALLOT_OBSERVATION_TYPE_INBOUND_TX,
			voters, 1, 50)
		require.NoError(t, err)
		require.Equal(t, int64(10), ballot.BlockHeightCreated)
		require.Equal(t, int64(60), ballot.BlockHeightExpiry)
	})

	t.Run("creating a new ballot expires stale active ballots", func(t *testing.T) {
		chainApp, ctx, validators := setupBallotTest(t, 2)
		k := chainApp.UvalidatorKeeper
		voters := voterAddrs(t, validators)

		// Create a ballot at height 1 with expiry after 1 block
		ctx = ctx.WithBlockHeight(1)
		_, err := k.CreateBallot(ctx, "old-ballot",
			uvalidatortypes.BallotObservationType_BALLOT_OBSERVATION_TYPE_INBOUND_TX,
			voters, 1, 1)
		require.NoError(t, err)

		// Advance height past the expiry so the next CreateBallot triggers cleanup
		ctx = ctx.WithBlockHeight(10)
		_, err = k.CreateBallot(ctx, "new-ballot",
			uvalidatortypes.BallotObservationType_BALLOT_OBSERVATION_TYPE_INBOUND_TX,
			voters, 1, 100)
		require.NoError(t, err)

		// Old ballot should now be expired
		old, err := k.GetBallot(ctx, "old-ballot")
		require.NoError(t, err)
		require.Equal(t, uvalidatortypes.BallotStatus_BALLOT_STATUS_EXPIRED, old.Status)
	})
}

// ─── GetOrCreateBallot ───────────────────────────────────────────────────────

func TestIntegration_GetOrCreateBallot(t *testing.T) {
	t.Run("creates a new ballot when none exists", func(t *testing.T) {
		chainApp, ctx, validators := setupBallotTest(t, 2)
		k := chainApp.UvalidatorKeeper
		voters := voterAddrs(t, validators)

		ballot, isNew, err := k.GetOrCreateBallot(ctx, "goc-new",
			uvalidatortypes.BallotObservationType_BALLOT_OBSERVATION_TYPE_INBOUND_TX,
			voters, 1, 100)
		require.NoError(t, err)
		require.True(t, isNew, "should report newly created")
		require.Equal(t, "goc-new", ballot.Id)
		require.Equal(t, uvalidatortypes.BallotStatus_BALLOT_STATUS_PENDING, ballot.Status)
	})

	t.Run("returns existing ballot without creating a duplicate", func(t *testing.T) {
		chainApp, ctx, validators := setupBallotTest(t, 2)
		k := chainApp.UvalidatorKeeper
		voters := voterAddrs(t, validators)

		// First call creates
		b1, isNew, err := k.GetOrCreateBallot(ctx, "goc-exist",
			uvalidatortypes.BallotObservationType_BALLOT_OBSERVATION_TYPE_INBOUND_TX,
			voters, 1, 100)
		require.NoError(t, err)
		require.True(t, isNew)

		// Second call returns existing, isNew == false
		b2, isNew, err := k.GetOrCreateBallot(ctx, "goc-exist",
			uvalidatortypes.BallotObservationType_BALLOT_OBSERVATION_TYPE_INBOUND_TX,
			voters, 1, 100)
		require.NoError(t, err)
		require.False(t, isNew, "should not report new on second call")
		require.Equal(t, b1.Id, b2.Id)
		require.Equal(t, b1.BlockHeightCreated, b2.BlockHeightCreated)
	})

	t.Run("second call does not overwrite existing ballot", func(t *testing.T) {
		chainApp, ctx, validators := setupBallotTest(t, 3)
		k := chainApp.UvalidatorKeeper
		voters := voterAddrs(t, validators)

		_, _, err := k.GetOrCreateBallot(ctx, "goc-nooverwrite",
			uvalidatortypes.BallotObservationType_BALLOT_OBSERVATION_TYPE_INBOUND_TX,
			voters[:1], 1, 100)
		require.NoError(t, err)

		// Second call with different voters — should still return the original
		b2, isNew, err := k.GetOrCreateBallot(ctx, "goc-nooverwrite",
			uvalidatortypes.BallotObservationType_BALLOT_OBSERVATION_TYPE_INBOUND_TX,
			voters, // different voter list
			2, 100)
		require.NoError(t, err)
		require.False(t, isNew)
		// Should have the original 1 eligible voter, not 3
		require.Len(t, b2.EligibleVoters, 1)
	})
}

// ─── GetBallot / SetBallot ───────────────────────────────────────────────────

func TestIntegration_GetBallot_SetBallot(t *testing.T) {
	t.Run("GetBallot returns error for non-existent ballot", func(t *testing.T) {
		chainApp, ctx, _ := setupBallotTest(t, 1)
		k := chainApp.UvalidatorKeeper

		_, err := k.GetBallot(ctx, "does-not-exist")
		require.Error(t, err)
	})

	t.Run("SetBallot persists updated status", func(t *testing.T) {
		chainApp, ctx, validators := setupBallotTest(t, 2)
		k := chainApp.UvalidatorKeeper
		voters := voterAddrs(t, validators)

		ballot, err := k.CreateBallot(ctx, "set-test",
			uvalidatortypes.BallotObservationType_BALLOT_OBSERVATION_TYPE_INBOUND_TX,
			voters, 1, 100)
		require.NoError(t, err)

		// Mutate and persist
		ballot.Status = uvalidatortypes.BallotStatus_BALLOT_STATUS_EXPIRED
		err = k.SetBallot(ctx, ballot)
		require.NoError(t, err)

		// Re-fetch and verify
		stored, err := k.GetBallot(ctx, "set-test")
		require.NoError(t, err)
		require.Equal(t, uvalidatortypes.BallotStatus_BALLOT_STATUS_EXPIRED, stored.Status)
	})

	t.Run("SetBallot round-trips all fields correctly", func(t *testing.T) {
		chainApp, ctx, validators := setupBallotTest(t, 3)
		k := chainApp.UvalidatorKeeper
		voters := voterAddrs(t, validators)

		original, err := k.CreateBallot(ctx, "roundtrip",
			uvalidatortypes.BallotObservationType_BALLOT_OBSERVATION_TYPE_INBOUND_TX,
			voters, 2, 50)
		require.NoError(t, err)

		stored, err := k.GetBallot(ctx, "roundtrip")
		require.NoError(t, err)
		require.Equal(t, original.Id, stored.Id)
		require.Equal(t, original.VotingThreshold, stored.VotingThreshold)
		require.Equal(t, original.BlockHeightExpiry, stored.BlockHeightExpiry)
		require.Equal(t, original.EligibleVoters, stored.EligibleVoters)
	})
}

// ─── DeleteBallot ────────────────────────────────────────────────────────────

func TestIntegration_DeleteBallot(t *testing.T) {
	t.Run("removes ballot from all collections", func(t *testing.T) {
		chainApp, ctx, validators := setupBallotTest(t, 2)
		k := chainApp.UvalidatorKeeper
		voters := voterAddrs(t, validators)

		_, err := k.CreateBallot(ctx, "del-1",
			uvalidatortypes.BallotObservationType_BALLOT_OBSERVATION_TYPE_INBOUND_TX,
			voters, 1, 100)
		require.NoError(t, err)

		// Confirm it exists and is active
		isActive, err := k.ActiveBallotIDs.Has(ctx, "del-1")
		require.NoError(t, err)
		require.True(t, isActive)

		// Delete
		err = k.DeleteBallot(ctx, "del-1")
		require.NoError(t, err)

		// Ballot should be gone
		_, err = k.GetBallot(ctx, "del-1")
		require.Error(t, err)

		// Should not appear in any collection
		isActive, err = k.ActiveBallotIDs.Has(ctx, "del-1")
		require.NoError(t, err)
		require.False(t, isActive)

		isExpired, err := k.ExpiredBallotIDs.Has(ctx, "del-1")
		require.NoError(t, err)
		require.False(t, isExpired)

		isFinalized, err := k.FinalizedBallotIDs.Has(ctx, "del-1")
		require.NoError(t, err)
		require.False(t, isFinalized)
	})

	t.Run("deleting a non-existent ballot does not error", func(t *testing.T) {
		chainApp, ctx, _ := setupBallotTest(t, 1)
		k := chainApp.UvalidatorKeeper

		// DeleteBallot on a missing key should be idempotent
		err := k.DeleteBallot(ctx, "ghost-ballot")
		require.NoError(t, err)
	})

	t.Run("deleting an expired ballot cleans up expired collection", func(t *testing.T) {
		chainApp, ctx, validators := setupBallotTest(t, 2)
		k := chainApp.UvalidatorKeeper
		voters := voterAddrs(t, validators)

		_, err := k.CreateBallot(ctx, "del-expired",
			uvalidatortypes.BallotObservationType_BALLOT_OBSERVATION_TYPE_INBOUND_TX,
			voters, 1, 1)
		require.NoError(t, err)

		// Mark it expired manually
		err = k.MarkBallotExpired(ctx, "del-expired")
		require.NoError(t, err)

		// Verify in expired set
		isExpired, err := k.ExpiredBallotIDs.Has(ctx, "del-expired")
		require.NoError(t, err)
		require.True(t, isExpired)

		// Delete
		err = k.DeleteBallot(ctx, "del-expired")
		require.NoError(t, err)

		isExpired, err = k.ExpiredBallotIDs.Has(ctx, "del-expired")
		require.NoError(t, err)
		require.False(t, isExpired)
	})
}

// ─── MarkBallotExpired ───────────────────────────────────────────────────────

func TestIntegration_MarkBallotExpired(t *testing.T) {
	t.Run("transitions ballot to EXPIRED and moves between collections", func(t *testing.T) {
		chainApp, ctx, validators := setupBallotTest(t, 2)
		k := chainApp.UvalidatorKeeper
		voters := voterAddrs(t, validators)

		_, err := k.CreateBallot(ctx, "expire-1",
			uvalidatortypes.BallotObservationType_BALLOT_OBSERVATION_TYPE_INBOUND_TX,
			voters, 1, 5)
		require.NoError(t, err)

		err = k.MarkBallotExpired(ctx, "expire-1")
		require.NoError(t, err)

		ballot, err := k.GetBallot(ctx, "expire-1")
		require.NoError(t, err)
		require.Equal(t, uvalidatortypes.BallotStatus_BALLOT_STATUS_EXPIRED, ballot.Status)

		// Should be in expired, NOT in active
		isExpired, err := k.ExpiredBallotIDs.Has(ctx, "expire-1")
		require.NoError(t, err)
		require.True(t, isExpired)

		isActive, err := k.ActiveBallotIDs.Has(ctx, "expire-1")
		require.NoError(t, err)
		require.False(t, isActive)
	})

	t.Run("expiring a non-existent ballot returns an error", func(t *testing.T) {
		chainApp, ctx, _ := setupBallotTest(t, 1)
		k := chainApp.UvalidatorKeeper

		err := k.MarkBallotExpired(ctx, "no-such-ballot")
		require.Error(t, err)
	})
}

// ─── MarkBallotFinalized ─────────────────────────────────────────────────────

func TestIntegration_MarkBallotFinalized(t *testing.T) {
	t.Run("finalizes ballot as PASSED", func(t *testing.T) {
		chainApp, ctx, validators := setupBallotTest(t, 2)
		k := chainApp.UvalidatorKeeper
		voters := voterAddrs(t, validators)

		_, err := k.CreateBallot(ctx, "finalize-pass",
			uvalidatortypes.BallotObservationType_BALLOT_OBSERVATION_TYPE_INBOUND_TX,
			voters, 1, 100)
		require.NoError(t, err)

		err = k.MarkBallotFinalized(ctx, "finalize-pass", uvalidatortypes.BallotStatus_BALLOT_STATUS_PASSED)
		require.NoError(t, err)

		ballot, err := k.GetBallot(ctx, "finalize-pass")
		require.NoError(t, err)
		require.Equal(t, uvalidatortypes.BallotStatus_BALLOT_STATUS_PASSED, ballot.Status)

		// In finalized, not in active
		isFinalized, err := k.FinalizedBallotIDs.Has(ctx, "finalize-pass")
		require.NoError(t, err)
		require.True(t, isFinalized)

		isActive, err := k.ActiveBallotIDs.Has(ctx, "finalize-pass")
		require.NoError(t, err)
		require.False(t, isActive)
	})

	t.Run("finalizes ballot as REJECTED", func(t *testing.T) {
		chainApp, ctx, validators := setupBallotTest(t, 2)
		k := chainApp.UvalidatorKeeper
		voters := voterAddrs(t, validators)

		_, err := k.CreateBallot(ctx, "finalize-reject",
			uvalidatortypes.BallotObservationType_BALLOT_OBSERVATION_TYPE_INBOUND_TX,
			voters, 1, 100)
		require.NoError(t, err)

		err = k.MarkBallotFinalized(ctx, "finalize-reject", uvalidatortypes.BallotStatus_BALLOT_STATUS_REJECTED)
		require.NoError(t, err)

		ballot, err := k.GetBallot(ctx, "finalize-reject")
		require.NoError(t, err)
		require.Equal(t, uvalidatortypes.BallotStatus_BALLOT_STATUS_REJECTED, ballot.Status)

		isFinalized, err := k.FinalizedBallotIDs.Has(ctx, "finalize-reject")
		require.NoError(t, err)
		require.True(t, isFinalized)
	})

	t.Run("invalid finalization status returns error", func(t *testing.T) {
		chainApp, ctx, validators := setupBallotTest(t, 1)
		k := chainApp.UvalidatorKeeper
		voters := voterAddrs(t, validators)

		_, err := k.CreateBallot(ctx, "finalize-bad",
			uvalidatortypes.BallotObservationType_BALLOT_OBSERVATION_TYPE_INBOUND_TX,
			voters, 1, 100)
		require.NoError(t, err)

		// PENDING is not a valid finalization status
		err = k.MarkBallotFinalized(ctx, "finalize-bad", uvalidatortypes.BallotStatus_BALLOT_STATUS_PENDING)
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid finalization status")

		// EXPIRED is not a valid finalization status either
		err = k.MarkBallotFinalized(ctx, "finalize-bad", uvalidatortypes.BallotStatus_BALLOT_STATUS_EXPIRED)
		require.Error(t, err)
	})

	t.Run("finalizing a non-existent ballot returns error", func(t *testing.T) {
		chainApp, ctx, _ := setupBallotTest(t, 1)
		k := chainApp.UvalidatorKeeper

		err := k.MarkBallotFinalized(ctx, "ghost", uvalidatortypes.BallotStatus_BALLOT_STATUS_PASSED)
		require.Error(t, err)
	})
}

// ─── ExpireBallotsBeforeHeight ───────────────────────────────────────────────

func TestIntegration_ExpireBallotsBeforeHeight(t *testing.T) {
	t.Run("expires ballots whose expiry height has passed", func(t *testing.T) {
		chainApp, ctx, validators := setupBallotTest(t, 2)
		k := chainApp.UvalidatorKeeper
		voters := voterAddrs(t, validators)

		// Block 1, expiry after 1 block → expires at height 2
		ctx = ctx.WithBlockHeight(1)
		b, err := k.CreateBallot(ctx, "auto-expire",
			uvalidatortypes.BallotObservationType_BALLOT_OBSERVATION_TYPE_INBOUND_TX,
			voters, 1, 1)
		require.NoError(t, err)
		require.Equal(t, int64(2), b.BlockHeightExpiry)

		// Call at height 5 (past expiry)
		err = k.ExpireBallotsBeforeHeight(ctx, 5)
		require.NoError(t, err)

		stored, err := k.GetBallot(ctx, "auto-expire")
		require.NoError(t, err)
		require.Equal(t, uvalidatortypes.BallotStatus_BALLOT_STATUS_EXPIRED, stored.Status)
	})

	t.Run("does not expire ballots whose expiry is in the future", func(t *testing.T) {
		chainApp, ctx, validators := setupBallotTest(t, 2)
		k := chainApp.UvalidatorKeeper
		voters := voterAddrs(t, validators)

		ctx = ctx.WithBlockHeight(1)
		_, err := k.CreateBallot(ctx, "not-yet-expired",
			uvalidatortypes.BallotObservationType_BALLOT_OBSERVATION_TYPE_INBOUND_TX,
			voters, 1, 1000)
		require.NoError(t, err)

		// Height 5 is well before expiry (height 1001)
		err = k.ExpireBallotsBeforeHeight(ctx, 5)
		require.NoError(t, err)

		stored, err := k.GetBallot(ctx, "not-yet-expired")
		require.NoError(t, err)
		require.Equal(t, uvalidatortypes.BallotStatus_BALLOT_STATUS_PENDING, stored.Status)
	})

	t.Run("selectively expires some ballots and leaves others pending", func(t *testing.T) {
		chainApp, ctx, validators := setupBallotTest(t, 2)
		k := chainApp.UvalidatorKeeper
		voters := voterAddrs(t, validators)

		ctx = ctx.WithBlockHeight(1)

		// 3 short-lived, 2 long-lived
		for i := 0; i < 3; i++ {
			_, err := k.CreateBallot(ctx, fmt.Sprintf("short-%d", i),
				uvalidatortypes.BallotObservationType_BALLOT_OBSERVATION_TYPE_INBOUND_TX,
				voters, 1, 1) // expires at height 2
			require.NoError(t, err)
		}
		for i := 0; i < 2; i++ {
			_, err := k.CreateBallot(ctx, fmt.Sprintf("long-%d", i),
				uvalidatortypes.BallotObservationType_BALLOT_OBSERVATION_TYPE_INBOUND_TX,
				voters, 1, 200) // expires at height 201
			require.NoError(t, err)
		}

		err := k.ExpireBallotsBeforeHeight(ctx, 10)
		require.NoError(t, err)

		for i := 0; i < 3; i++ {
			b, err := k.GetBallot(ctx, fmt.Sprintf("short-%d", i))
			require.NoError(t, err)
			require.Equal(t, uvalidatortypes.BallotStatus_BALLOT_STATUS_EXPIRED, b.Status)
		}
		for i := 0; i < 2; i++ {
			b, err := k.GetBallot(ctx, fmt.Sprintf("long-%d", i))
			require.NoError(t, err)
			require.Equal(t, uvalidatortypes.BallotStatus_BALLOT_STATUS_PENDING, b.Status)
		}
	})

	t.Run("no-op when there are no active ballots", func(t *testing.T) {
		chainApp, ctx, _ := setupBallotTest(t, 1)
		k := chainApp.UvalidatorKeeper

		err := k.ExpireBallotsBeforeHeight(ctx, 9999)
		require.NoError(t, err)
	})

	t.Run("boundary: expires ballot at exactly the expiry height", func(t *testing.T) {
		chainApp, ctx, validators := setupBallotTest(t, 2)
		k := chainApp.UvalidatorKeeper
		voters := voterAddrs(t, validators)

		ctx = ctx.WithBlockHeight(1)
		b, err := k.CreateBallot(ctx, "boundary-ballot",
			uvalidatortypes.BallotObservationType_BALLOT_OBSERVATION_TYPE_INBOUND_TX,
			voters, 1, 9) // expiry = 1 + 9 = 10
		require.NoError(t, err)
		require.Equal(t, int64(10), b.BlockHeightExpiry)

		// currentHeight == expiry height → should expire (<=)
		err = k.ExpireBallotsBeforeHeight(ctx, 10)
		require.NoError(t, err)

		stored, err := k.GetBallot(ctx, "boundary-ballot")
		require.NoError(t, err)
		require.Equal(t, uvalidatortypes.BallotStatus_BALLOT_STATUS_EXPIRED, stored.Status)
	})
}

// ─── AddVoteToBallot ─────────────────────────────────────────────────────────

func TestIntegration_AddVoteToBallot(t *testing.T) {
	t.Run("records a YES vote and persists the ballot", func(t *testing.T) {
		chainApp, ctx, validators := setupBallotTest(t, 3)
		k := chainApp.UvalidatorKeeper
		voters := voterAddrs(t, validators)

		ballot, err := k.CreateBallot(ctx, "addvote-yes",
			uvalidatortypes.BallotObservationType_BALLOT_OBSERVATION_TYPE_INBOUND_TX,
			voters, 2, 100)
		require.NoError(t, err)

		updated, err := k.AddVoteToBallot(ctx, ballot, voters[0], uvalidatortypes.VoteResult_VOTE_RESULT_SUCCESS)
		require.NoError(t, err)
		require.Equal(t, uvalidatortypes.VoteResult_VOTE_RESULT_SUCCESS, updated.Votes[0])

		// Verify persistence
		stored, err := k.GetBallot(ctx, "addvote-yes")
		require.NoError(t, err)
		require.Equal(t, uvalidatortypes.VoteResult_VOTE_RESULT_SUCCESS, stored.Votes[0])
	})

	t.Run("records a NO vote and persists the ballot", func(t *testing.T) {
		chainApp, ctx, validators := setupBallotTest(t, 3)
		k := chainApp.UvalidatorKeeper
		voters := voterAddrs(t, validators)

		ballot, err := k.CreateBallot(ctx, "addvote-no",
			uvalidatortypes.BallotObservationType_BALLOT_OBSERVATION_TYPE_INBOUND_TX,
			voters, 2, 100)
		require.NoError(t, err)

		updated, err := k.AddVoteToBallot(ctx, ballot, voters[1], uvalidatortypes.VoteResult_VOTE_RESULT_FAILURE)
		require.NoError(t, err)
		require.Equal(t, uvalidatortypes.VoteResult_VOTE_RESULT_FAILURE, updated.Votes[1])
	})

	t.Run("rejects vote from ineligible voter", func(t *testing.T) {
		chainApp, ctx, validators := setupBallotTest(t, 2)
		k := chainApp.UvalidatorKeeper
		voters := voterAddrs(t, validators)

		ballot, err := k.CreateBallot(ctx, "addvote-ineligible",
			uvalidatortypes.BallotObservationType_BALLOT_OBSERVATION_TYPE_INBOUND_TX,
			voters[:1], 1, 100) // only voters[0] is eligible
		require.NoError(t, err)

		_, err = k.AddVoteToBallot(ctx, ballot, voters[1], uvalidatortypes.VoteResult_VOTE_RESULT_SUCCESS)
		require.Error(t, err)
		require.Contains(t, err.Error(), "not eligible")
	})

	t.Run("rejects double vote from same voter", func(t *testing.T) {
		chainApp, ctx, validators := setupBallotTest(t, 2)
		k := chainApp.UvalidatorKeeper
		voters := voterAddrs(t, validators)

		ballot, err := k.CreateBallot(ctx, "addvote-double",
			uvalidatortypes.BallotObservationType_BALLOT_OBSERVATION_TYPE_INBOUND_TX,
			voters, 1, 100)
		require.NoError(t, err)

		ballot, err = k.AddVoteToBallot(ctx, ballot, voters[0], uvalidatortypes.VoteResult_VOTE_RESULT_SUCCESS)
		require.NoError(t, err)

		// Try voting again
		_, err = k.AddVoteToBallot(ctx, ballot, voters[0], uvalidatortypes.VoteResult_VOTE_RESULT_SUCCESS)
		require.Error(t, err)
		require.Contains(t, err.Error(), "already voted")
	})
}

// ─── CheckIfFinalizingVote ───────────────────────────────────────────────────

func TestIntegration_CheckIfFinalizingVote(t *testing.T) {
	t.Run("not finalizing when threshold not met", func(t *testing.T) {
		chainApp, ctx, validators := setupBallotTest(t, 3)
		k := chainApp.UvalidatorKeeper
		voters := voterAddrs(t, validators)

		ballot, err := k.CreateBallot(ctx, "fincheck-pending",
			uvalidatortypes.BallotObservationType_BALLOT_OBSERVATION_TYPE_INBOUND_TX,
			voters, 3, 100) // need 3 YES to pass
		require.NoError(t, err)

		// Add only 1 YES vote
		ballot, err = k.AddVoteToBallot(ctx, ballot, voters[0], uvalidatortypes.VoteResult_VOTE_RESULT_SUCCESS)
		require.NoError(t, err)

		_, isFinalizing, err := k.CheckIfFinalizingVote(ctx, ballot)
		require.NoError(t, err)
		require.False(t, isFinalizing)
	})

	t.Run("finalizing YES when threshold met", func(t *testing.T) {
		chainApp, ctx, validators := setupBallotTest(t, 3)
		k := chainApp.UvalidatorKeeper
		voters := voterAddrs(t, validators)

		ballot, err := k.CreateBallot(ctx, "fincheck-pass",
			uvalidatortypes.BallotObservationType_BALLOT_OBSERVATION_TYPE_INBOUND_TX,
			voters, 2, 100) // need 2 YES votes
		require.NoError(t, err)

		ballot, err = k.AddVoteToBallot(ctx, ballot, voters[0], uvalidatortypes.VoteResult_VOTE_RESULT_SUCCESS)
		require.NoError(t, err)
		ballot, err = k.AddVoteToBallot(ctx, ballot, voters[1], uvalidatortypes.VoteResult_VOTE_RESULT_SUCCESS)
		require.NoError(t, err)

		finalizedBallot, isFinalizing, err := k.CheckIfFinalizingVote(ctx, ballot)
		require.NoError(t, err)
		require.True(t, isFinalizing)
		require.Equal(t, uvalidatortypes.BallotStatus_BALLOT_STATUS_PASSED, finalizedBallot.Status)
	})

	t.Run("finalizing NO (REJECTED) when no votes make threshold unreachable", func(t *testing.T) {
		chainApp, ctx, validators := setupBallotTest(t, 3)
		k := chainApp.UvalidatorKeeper
		voters := voterAddrs(t, validators)

		// 3 voters, need 3 YES to pass → after 1 NO, only 2 possible YES, threshold impossible
		ballot, err := k.CreateBallot(ctx, "fincheck-reject",
			uvalidatortypes.BallotObservationType_BALLOT_OBSERVATION_TYPE_INBOUND_TX,
			voters, 3, 100)
		require.NoError(t, err)

		// Cast enough NO votes to make passing impossible (need 3 YES but 3 NO means 0 YES possible)
		for _, voter := range voters {
			ballot, err = k.AddVoteToBallot(ctx, ballot, voter, uvalidatortypes.VoteResult_VOTE_RESULT_FAILURE)
			require.NoError(t, err)
		}

		finalizedBallot, isFinalizing, err := k.CheckIfFinalizingVote(ctx, ballot)
		require.NoError(t, err)
		require.True(t, isFinalizing)
		require.Equal(t, uvalidatortypes.BallotStatus_BALLOT_STATUS_REJECTED, finalizedBallot.Status)
	})

	t.Run("already-finalized ballot is not re-finalized", func(t *testing.T) {
		chainApp, ctx, validators := setupBallotTest(t, 2)
		k := chainApp.UvalidatorKeeper
		voters := voterAddrs(t, validators)

		ballot, err := k.CreateBallot(ctx, "fincheck-already",
			uvalidatortypes.BallotObservationType_BALLOT_OBSERVATION_TYPE_INBOUND_TX,
			voters, 1, 100)
		require.NoError(t, err)

		// Finalize it
		err = k.MarkBallotFinalized(ctx, "fincheck-already", uvalidatortypes.BallotStatus_BALLOT_STATUS_PASSED)
		require.NoError(t, err)

		ballot, err = k.GetBallot(ctx, "fincheck-already")
		require.NoError(t, err)

		_, isFinalizing, err := k.CheckIfFinalizingVote(ctx, ballot)
		require.NoError(t, err)
		require.False(t, isFinalizing, "already-finalized ballot should not trigger again")
	})
}

// ─── VoteOnBallot ─────────────────────────────────────────────────────────────

func TestIntegration_VoteOnBallot(t *testing.T) {
	t.Run("creates ballot on first vote", func(t *testing.T) {
		chainApp, ctx, validators := setupBallotTest(t, 3)
		k := chainApp.UvalidatorKeeper
		voters := voterAddrs(t, validators)

		ballot, isFinalized, isNew, err := k.VoteOnBallot(ctx,
			"vote-new", uvalidatortypes.BallotObservationType_BALLOT_OBSERVATION_TYPE_INBOUND_TX,
			voters[0], uvalidatortypes.VoteResult_VOTE_RESULT_SUCCESS,
			voters, 2, 100)
		require.NoError(t, err)
		require.True(t, isNew)
		require.False(t, isFinalized)
		require.Equal(t, uvalidatortypes.BallotStatus_BALLOT_STATUS_PENDING, ballot.Status)
	})

	t.Run("uses existing ballot on subsequent vote", func(t *testing.T) {
		chainApp, ctx, validators := setupBallotTest(t, 3)
		k := chainApp.UvalidatorKeeper
		voters := voterAddrs(t, validators)

		_, _, _, err := k.VoteOnBallot(ctx,
			"vote-exist", uvalidatortypes.BallotObservationType_BALLOT_OBSERVATION_TYPE_INBOUND_TX,
			voters[0], uvalidatortypes.VoteResult_VOTE_RESULT_SUCCESS,
			voters, 2, 100)
		require.NoError(t, err)

		_, _, isNew, err := k.VoteOnBallot(ctx,
			"vote-exist", uvalidatortypes.BallotObservationType_BALLOT_OBSERVATION_TYPE_INBOUND_TX,
			voters[1], uvalidatortypes.VoteResult_VOTE_RESULT_SUCCESS,
			voters, 2, 100)
		require.NoError(t, err)
		require.False(t, isNew, "second vote should use existing ballot")
	})

	t.Run("finalizes ballot when threshold is reached", func(t *testing.T) {
		chainApp, ctx, validators := setupBallotTest(t, 3)
		k := chainApp.UvalidatorKeeper
		voters := voterAddrs(t, validators)

		// Threshold = 2, cast 2 YES votes
		_, _, _, err := k.VoteOnBallot(ctx,
			"vote-finalize", uvalidatortypes.BallotObservationType_BALLOT_OBSERVATION_TYPE_INBOUND_TX,
			voters[0], uvalidatortypes.VoteResult_VOTE_RESULT_SUCCESS,
			voters, 2, 100)
		require.NoError(t, err)

		ballot, isFinalized, _, err := k.VoteOnBallot(ctx,
			"vote-finalize", uvalidatortypes.BallotObservationType_BALLOT_OBSERVATION_TYPE_INBOUND_TX,
			voters[1], uvalidatortypes.VoteResult_VOTE_RESULT_SUCCESS,
			voters, 2, 100)
		require.NoError(t, err)
		require.True(t, isFinalized)
		require.Equal(t, uvalidatortypes.BallotStatus_BALLOT_STATUS_PASSED, ballot.Status)

		// Finalized ballot should be in FinalizedBallotIDs
		isInFinalized, err := k.FinalizedBallotIDs.Has(ctx, "vote-finalize")
		require.NoError(t, err)
		require.True(t, isInFinalized)

		// Should not be in active
		isActive, err := k.ActiveBallotIDs.Has(ctx, "vote-finalize")
		require.NoError(t, err)
		require.False(t, isActive)
	})

	t.Run("rejected ballot when NO votes make threshold impossible", func(t *testing.T) {
		chainApp, ctx, validators := setupBallotTest(t, 3)
		k := chainApp.UvalidatorKeeper
		voters := voterAddrs(t, validators)

		// 3 voters, threshold 3 — cast 1 NO makes it impossible (max YES = 2 < 3)
		_, _, _, err := k.VoteOnBallot(ctx,
			"vote-reject", uvalidatortypes.BallotObservationType_BALLOT_OBSERVATION_TYPE_INBOUND_TX,
			voters[0], uvalidatortypes.VoteResult_VOTE_RESULT_FAILURE,
			voters, 3, 100)
		require.NoError(t, err)

		// second NO: max possible YES = 1, still < 3
		_, _, _, err = k.VoteOnBallot(ctx,
			"vote-reject", uvalidatortypes.BallotObservationType_BALLOT_OBSERVATION_TYPE_INBOUND_TX,
			voters[1], uvalidatortypes.VoteResult_VOTE_RESULT_FAILURE,
			voters, 3, 100)
		require.NoError(t, err)

		// third NO finalizes as REJECTED
		ballot, isFinalized, _, err := k.VoteOnBallot(ctx,
			"vote-reject", uvalidatortypes.BallotObservationType_BALLOT_OBSERVATION_TYPE_INBOUND_TX,
			voters[2], uvalidatortypes.VoteResult_VOTE_RESULT_FAILURE,
			voters, 3, 100)
		require.NoError(t, err)
		require.True(t, isFinalized)
		require.Equal(t, uvalidatortypes.BallotStatus_BALLOT_STATUS_REJECTED, ballot.Status)
	})

	t.Run("voting on already-finalized ballot returns error", func(t *testing.T) {
		chainApp, ctx, validators := setupBallotTest(t, 2)
		k := chainApp.UvalidatorKeeper
		voters := voterAddrs(t, validators)

		// Finalize via threshold
		_, _, _, err := k.VoteOnBallot(ctx,
			"vote-already-done", uvalidatortypes.BallotObservationType_BALLOT_OBSERVATION_TYPE_INBOUND_TX,
			voters[0], uvalidatortypes.VoteResult_VOTE_RESULT_SUCCESS,
			voters, 1, 100) // threshold 1 → finalized after first vote
		require.NoError(t, err)

		// Attempt to vote again on the finalized ballot
		_, _, _, err = k.VoteOnBallot(ctx,
			"vote-already-done", uvalidatortypes.BallotObservationType_BALLOT_OBSERVATION_TYPE_INBOUND_TX,
			voters[1], uvalidatortypes.VoteResult_VOTE_RESULT_SUCCESS,
			voters, 1, 100)
		require.Error(t, err)
		require.Contains(t, err.Error(), "already")
	})

	t.Run("full multi-voter flow: each voter votes once, ballot finalizes", func(t *testing.T) {
		chainApp, ctx, validators := setupBallotTest(t, 4)
		k := chainApp.UvalidatorKeeper
		voters := voterAddrs(t, validators)
		const threshold = int64(3)

		var lastBallot uvalidatortypes.Ballot
		var finalizedSeen bool
		for i, voter := range voters {
			b, isFin, _, err := k.VoteOnBallot(ctx,
				"vote-multi", uvalidatortypes.BallotObservationType_BALLOT_OBSERVATION_TYPE_INBOUND_TX,
				voter, uvalidatortypes.VoteResult_VOTE_RESULT_SUCCESS,
				voters, threshold, 100)
			if int64(i+1) > threshold {
				// After finalization, further votes return an error
				require.Error(t, err)
				break
			}
			require.NoError(t, err)
			lastBallot = b
			finalizedSeen = finalizedSeen || isFin
		}
		require.True(t, finalizedSeen, "ballot should have been finalized when threshold was reached")
		require.Equal(t, uvalidatortypes.BallotStatus_BALLOT_STATUS_PASSED, lastBallot.Status)
	})
}

// ─── IsBondedUniversalValidator ──────────────────────────────────────────────

func TestIntegration_IsBondedUniversalValidator(t *testing.T) {
	t.Run("returns true for a bonded universal validator", func(t *testing.T) {
		chainApp, ctx, validators := setupBallotTest(t, 2)
		k := chainApp.UvalidatorKeeper

		val := validators[0]
		registerUniversalValidator(t, chainApp, ctx, val)

		valAddr, err := sdk.ValAddressFromBech32(val.OperatorAddress)
		require.NoError(t, err)
		accAddr := sdk.AccAddress(valAddr)

		isBonded, err := k.IsBondedUniversalValidator(ctx, accAddr.String())
		require.NoError(t, err)
		require.True(t, isBonded)
	})

	t.Run("returns false for a registered but non-bonded universal validator", func(t *testing.T) {
		chainApp, ctx, validators := setupBallotTest(t, 2)
		k := chainApp.UvalidatorKeeper

		val := validators[0]
		registerUniversalValidator(t, chainApp, ctx, val)

		// Force unbond the underlying staking validator
		valAddr, err := sdk.ValAddressFromBech32(val.OperatorAddress)
		require.NoError(t, err)
		unbondedVal := val
		unbondedVal.Status = stakingtypes.Unbonded
		chainApp.StakingKeeper.SetValidator(ctx, unbondedVal)

		accAddr := sdk.AccAddress(valAddr)
		isBonded, err := k.IsBondedUniversalValidator(ctx, accAddr.String())
		require.NoError(t, err)
		require.False(t, isBonded)
	})

	t.Run("returns error for a validator not in the universal validator set", func(t *testing.T) {
		chainApp, ctx, validators := setupBallotTest(t, 2)
		k := chainApp.UvalidatorKeeper

		// val is a staking validator but NOT registered as a universal validator
		val := validators[1]
		valAddr, err := sdk.ValAddressFromBech32(val.OperatorAddress)
		require.NoError(t, err)
		accAddr := sdk.AccAddress(valAddr)

		_, err = k.IsBondedUniversalValidator(ctx, accAddr.String())
		require.Error(t, err)
		require.Contains(t, err.Error(), "not present in the registered universal validators set")
	})

	t.Run("returns error for an invalid bech32 address", func(t *testing.T) {
		chainApp, ctx, _ := setupBallotTest(t, 1)
		k := chainApp.UvalidatorKeeper

		_, err := k.IsBondedUniversalValidator(ctx, "not-a-valid-address")
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid signer address")
	})
}

// ─── IsTombstonedUniversalValidator ──────────────────────────────────────────

func TestIntegration_IsTombstonedUniversalValidator(t *testing.T) {
	t.Run("returns false for a live (non-tombstoned) universal validator", func(t *testing.T) {
		chainApp, ctx, validators := setupBallotTest(t, 2)
		k := chainApp.UvalidatorKeeper

		val := validators[0]
		registerUniversalValidator(t, chainApp, ctx, val)

		valAddr, err := sdk.ValAddressFromBech32(val.OperatorAddress)
		require.NoError(t, err)
		accAddr := sdk.AccAddress(valAddr)

		isTombstoned, err := k.IsTombstonedUniversalValidator(ctx, accAddr.String())
		require.NoError(t, err)
		require.False(t, isTombstoned)
	})

	t.Run("returns error for a validator not in the universal validator set", func(t *testing.T) {
		chainApp, ctx, validators := setupBallotTest(t, 2)
		k := chainApp.UvalidatorKeeper

		// Not registered as UV
		val := validators[0]
		valAddr, err := sdk.ValAddressFromBech32(val.OperatorAddress)
		require.NoError(t, err)
		accAddr := sdk.AccAddress(valAddr)

		_, err = k.IsTombstonedUniversalValidator(ctx, accAddr.String())
		require.Error(t, err)
		require.Contains(t, err.Error(), "not present in the registered universal validators set")
	})

	t.Run("returns error for an invalid bech32 address", func(t *testing.T) {
		chainApp, ctx, _ := setupBallotTest(t, 1)
		k := chainApp.UvalidatorKeeper

		_, err := k.IsTombstonedUniversalValidator(ctx, "bad-address!!!")
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid signer address")
	})
}
