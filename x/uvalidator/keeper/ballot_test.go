package keeper_test

import (
	"fmt"
	"testing"

	"github.com/pushchain/push-chain-node/x/uvalidator/types"
	"github.com/stretchr/testify/require"
)

func TestCreateAndGetBallot(t *testing.T) {
	f := SetupTest(t)
	require := require.New(t)

	// Success
	b, err := f.k.CreateBallot(f.ctx, "b1", types.BallotObservationType_BALLOT_OBSERVATION_TYPE_INBOUND_TX,
		[]string{"v1", "v2"}, 1, 10)
	require.NoError(err)
	require.Equal("b1", b.Id)

	got, err := f.k.GetBallot(f.ctx, "b1")
	require.NoError(err)
	require.Equal(b.Id, got.Id)

	// Error: get non-existent ballot
	_, err = f.k.GetBallot(f.ctx, "does-not-exist")
	require.Error(err)
}

func TestGetOrCreateBallot(t *testing.T) {
	f := SetupTest(t)
	require := require.New(t)

	// First call creates
	b1, created, err := f.k.GetOrCreateBallot(f.ctx, "b2",
		types.BallotObservationType_BALLOT_OBSERVATION_TYPE_INBOUND_TX, []string{"v1"}, 1, 5)
	require.NoError(err)
	require.True(created)

	// Second call returns existing
	b2, created, err := f.k.GetOrCreateBallot(f.ctx, "b2",
		types.BallotObservationType_BALLOT_OBSERVATION_TYPE_INBOUND_TX, []string{"v1"}, 1, 5)
	require.NoError(err)
	require.False(created)
	require.Equal(b1.Id, b2.Id)
}

func TestSetAndDeleteBallot(t *testing.T) {
	f := SetupTest(t)
	require := require.New(t)

	b, err := f.k.CreateBallot(f.ctx, "b3", types.BallotObservationType_BALLOT_OBSERVATION_TYPE_INBOUND_TX,
		[]string{"v1"}, 1, 5)
	require.NoError(err)

	// Update ballot status manually
	b.Status = types.BallotStatus_BALLOT_STATUS_EXPIRED
	err = f.k.SetBallot(f.ctx, b)
	require.NoError(err)

	got, err := f.k.GetBallot(f.ctx, "b3")
	require.NoError(err)
	require.Equal(types.BallotStatus_BALLOT_STATUS_EXPIRED, got.Status)

	// Delete ballot
	err = f.k.DeleteBallot(f.ctx, "b3")
	require.NoError(err)

	_, err = f.k.GetBallot(f.ctx, "b3")
	require.Error(err)

	// Deleting again should not error badly
	err = f.k.DeleteBallot(f.ctx, "b3")
	require.NoError(err)
}

func TestMarkBallotExpired(t *testing.T) {
	f := SetupTest(t)
	require := require.New(t)

	b, err := f.k.CreateBallot(f.ctx, "b4", types.BallotObservationType_BALLOT_OBSERVATION_TYPE_INBOUND_TX,
		[]string{"v1"}, 1, 5)
	require.NoError(err)

	// Expire ballot
	err = f.k.MarkBallotExpired(f.ctx, b.Id)
	require.NoError(err)

	got, err := f.k.GetBallot(f.ctx, b.Id)
	require.NoError(err)
	require.Equal(types.BallotStatus_BALLOT_STATUS_EXPIRED, got.Status)

	// Expiring non-existent ballot should error
	err = f.k.MarkBallotExpired(f.ctx, "no-ballot")
	require.Error(err)
}

func TestMarkBallotFinalized(t *testing.T) {
	f := SetupTest(t)
	require := require.New(t)

	b, err := f.k.CreateBallot(f.ctx, "b5", types.BallotObservationType_BALLOT_OBSERVATION_TYPE_INBOUND_TX,
		[]string{"v1"}, 1, 5)
	require.NoError(err)

	// Finalize as PASSED
	err = f.k.MarkBallotFinalized(f.ctx, b.Id, types.BallotStatus_BALLOT_STATUS_PASSED)
	require.NoError(err)

	got, err := f.k.GetBallot(f.ctx, b.Id)
	require.NoError(err)
	require.Equal(types.BallotStatus_BALLOT_STATUS_PASSED, got.Status)

	// Invalid finalization status should error
	err = f.k.MarkBallotFinalized(f.ctx, b.Id, types.BallotStatus_BALLOT_STATUS_PENDING)
	require.Error(err)
}

func TestExpireBallotsBeforeHeight(t *testing.T) {
	f := SetupTest(t)
	require := require.New(t)

	// Create ballot that expires at +1 block
	b, err := f.k.CreateBallot(f.ctx, "b6", types.BallotObservationType_BALLOT_OBSERVATION_TYPE_INBOUND_TX,
		[]string{"v1"}, 1, 1)
	require.NoError(err)

	// Sanity check: expiry is after created height
	require.Equal(b.BlockHeightCreated+1, b.BlockHeightExpiry)

	// Expire with currentHeight past expiry
	err = f.k.ExpireBallotsBeforeHeight(f.ctx, b.BlockHeightExpiry+1)
	require.NoError(err)

	got, err := f.k.GetBallot(f.ctx, b.Id)
	require.NoError(err)
	require.Equal(types.BallotStatus_BALLOT_STATUS_EXPIRED, got.Status)

	// Create another ballot with long expiry
	b2, err := f.k.CreateBallot(f.ctx, "b7", types.BallotObservationType_BALLOT_OBSERVATION_TYPE_INBOUND_TX,
		[]string{"v1"}, 1, 10)
	require.NoError(err)

	// Not yet expired
	err = f.k.ExpireBallotsBeforeHeight(f.ctx, b2.BlockHeightCreated+1)
	require.NoError(err)

	got2, err := f.k.GetBallot(f.ctx, "b7")
	require.NoError(err)
	require.Equal(types.BallotStatus_BALLOT_STATUS_PENDING, got2.Status)
}

func TestCreateBallot_ExpiresOldOnCreate(t *testing.T) {
	f := SetupTest(t)
	require := require.New(t)

	// Create a ballot that expires quickly (expiry = 1 block)
	oldBallot, err := f.k.CreateBallot(f.ctx, "old",
		types.BallotObservationType_BALLOT_OBSERVATION_TYPE_INBOUND_TX,
		[]string{"v1"}, 1, 1)
	require.NoError(err)

	// Manually simulate advancing block height
	f.ctx = f.ctx.WithBlockHeight(oldBallot.BlockHeightCreated + 5)

	// Now create a NEW ballot → should trigger expiry cleanup of the old one
	newBallot, err := f.k.CreateBallot(f.ctx, "new",
		types.BallotObservationType_BALLOT_OBSERVATION_TYPE_INBOUND_TX,
		[]string{"v2"}, 1, 10)
	require.NoError(err)

	// New ballot must be created fine
	require.Equal("new", newBallot.Id)

	// Old ballot should now be expired
	got, err := f.k.GetBallot(f.ctx, "old")
	require.NoError(err)
	require.Equal(types.BallotStatus_BALLOT_STATUS_EXPIRED, got.Status)
}

func TestCreateBallot_NoExpiryTriggered(t *testing.T) {
	f := SetupTest(t)
	require := require.New(t)

	// Create a long-lived ballot
	b1, err := f.k.CreateBallot(f.ctx, "long",
		types.BallotObservationType_BALLOT_OBSERVATION_TYPE_INBOUND_TX,
		[]string{"v1"}, 1, 100)
	require.NoError(err)

	// Advance block height, but not beyond expiry
	f.ctx = f.ctx.WithBlockHeight(b1.BlockHeightCreated + 10)

	// Create another ballot → should not expire the first one
	_, err = f.k.CreateBallot(f.ctx, "newer",
		types.BallotObservationType_BALLOT_OBSERVATION_TYPE_INBOUND_TX,
		[]string{"v2"}, 1, 50)
	require.NoError(err)

	// Check both ballots
	got1, err := f.k.GetBallot(f.ctx, "long")
	require.NoError(err)
	require.Equal(types.BallotStatus_BALLOT_STATUS_PENDING, got1.Status)

	got2, err := f.k.GetBallot(f.ctx, "newer")
	require.NoError(err)
	require.Equal("newer", got2.Id)
}

func TestCreateBallot_ExpiresMultipleOld(t *testing.T) {
	f := SetupTest(t)
	require := require.New(t)

	// Two short-lived ballots
	b1, err := f.k.CreateBallot(f.ctx, "old1",
		types.BallotObservationType_BALLOT_OBSERVATION_TYPE_INBOUND_TX,
		[]string{"v1"}, 1, 1)
	require.NoError(err)

	b2, err := f.k.CreateBallot(f.ctx, "old2",
		types.BallotObservationType_BALLOT_OBSERVATION_TYPE_INBOUND_TX,
		[]string{"v2"}, 1, 1)
	require.NoError(err)

	// Advance height beyond both expiries
	f.ctx = f.ctx.WithBlockHeight(b2.BlockHeightCreated + 5)

	// Create fresh ballot (triggers cleanup)
	_, err = f.k.CreateBallot(f.ctx, "fresh",
		types.BallotObservationType_BALLOT_OBSERVATION_TYPE_INBOUND_TX,
		[]string{"v3"}, 1, 5)
	require.NoError(err)

	// Both old ballots should now be expired
	got1, err := f.k.GetBallot(f.ctx, b1.Id)
	require.NoError(err)
	require.Equal(types.BallotStatus_BALLOT_STATUS_EXPIRED, got1.Status)

	got2, err := f.k.GetBallot(f.ctx, b2.Id)
	require.NoError(err)
	require.Equal(types.BallotStatus_BALLOT_STATUS_EXPIRED, got2.Status)
}

func TestExpireBallotsBeforeHeight_MultipleExpiredAndPending(t *testing.T) {
	f := SetupTest(t)
	require := require.New(t)

	// Create 5 active ballots: 3 with short expiry (1 block), 2 with long expiry (100 blocks)
	// Context starts at height 0, so ballots get created at height 0.
	shortExpiry := int64(1) // BlockHeightExpiry = 0 + 1 = 1
	longExpiry := int64(100) // BlockHeightExpiry = 0 + 100 = 100

	for i := 0; i < 3; i++ {
		_, err := f.k.CreateBallot(f.ctx, fmt.Sprintf("short-%d", i),
			types.BallotObservationType_BALLOT_OBSERVATION_TYPE_INBOUND_TX,
			[]string{"v1"}, 1, shortExpiry)
		require.NoError(err)
	}
	for i := 0; i < 2; i++ {
		_, err := f.k.CreateBallot(f.ctx, fmt.Sprintf("long-%d", i),
			types.BallotObservationType_BALLOT_OBSERVATION_TYPE_INBOUND_TX,
			[]string{"v1"}, 1, longExpiry)
		require.NoError(err)
	}

	// Expire at height 5 — short-expiry ballots (expiry=1) should expire, long ones (expiry=100) should not
	err := f.k.ExpireBallotsBeforeHeight(f.ctx, 5)
	require.NoError(err)

	// Assert all 3 short-expiry ballots are expired
	for i := 0; i < 3; i++ {
		id := fmt.Sprintf("short-%d", i)
		got, err := f.k.GetBallot(f.ctx, id)
		require.NoError(err)
		require.Equal(types.BallotStatus_BALLOT_STATUS_EXPIRED, got.Status, "ballot %s should be expired", id)

		has, err := f.k.ExpiredBallotIDs.Has(f.ctx, id)
		require.NoError(err)
		require.True(has, "ballot %s should be in ExpiredBallotIDs", id)

		has, err = f.k.ActiveBallotIDs.Has(f.ctx, id)
		require.NoError(err)
		require.False(has, "ballot %s should not be in ActiveBallotIDs", id)
	}

	// Assert 2 long-expiry ballots are still pending
	for i := 0; i < 2; i++ {
		id := fmt.Sprintf("long-%d", i)
		got, err := f.k.GetBallot(f.ctx, id)
		require.NoError(err)
		require.Equal(types.BallotStatus_BALLOT_STATUS_PENDING, got.Status, "ballot %s should still be pending", id)

		has, err := f.k.ActiveBallotIDs.Has(f.ctx, id)
		require.NoError(err)
		require.True(has, "ballot %s should still be in ActiveBallotIDs", id)
	}
}

func TestExpireBallotsBeforeHeight_NoneSkipped(t *testing.T) {
	f := SetupTest(t)
	require := require.New(t)

	n := 10
	// Create N active ballots all with expiry at height 1
	for i := 0; i < n; i++ {
		_, err := f.k.CreateBallot(f.ctx, fmt.Sprintf("ballot-%d", i),
			types.BallotObservationType_BALLOT_OBSERVATION_TYPE_INBOUND_TX,
			[]string{"v1"}, 1, 1)
		require.NoError(err)
	}

	// Expire all at height 5
	err := f.k.ExpireBallotsBeforeHeight(f.ctx, 5)
	require.NoError(err)

	// Assert ALL N ballots are expired — none skipped
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("ballot-%d", i)
		got, err := f.k.GetBallot(f.ctx, id)
		require.NoError(err)
		require.Equal(types.BallotStatus_BALLOT_STATUS_EXPIRED, got.Status, "ballot %s should be expired", id)

		has, err := f.k.ExpiredBallotIDs.Has(f.ctx, id)
		require.NoError(err)
		require.True(has, "ballot %s should be in ExpiredBallotIDs", id)

		has, err = f.k.ActiveBallotIDs.Has(f.ctx, id)
		require.NoError(err)
		require.False(has, "ballot %s should not be in ActiveBallotIDs", id)
	}
}

func TestExpireBallotsBeforeHeight_BoundaryHeight(t *testing.T) {
	f := SetupTest(t)
	require := require.New(t)

	// Create a ballot with expiry at height 10 (created at 0, expiryAfterBlocks=10)
	_, err := f.k.CreateBallot(f.ctx, "at-boundary",
		types.BallotObservationType_BALLOT_OBSERVATION_TYPE_INBOUND_TX,
		[]string{"v1"}, 1, 10)
	require.NoError(err)

	// Ballot with expiry at height 9 (created at 0, expiryAfterBlocks=9)
	_, err = f.k.CreateBallot(f.ctx, "below-boundary",
		types.BallotObservationType_BALLOT_OBSERVATION_TYPE_INBOUND_TX,
		[]string{"v1"}, 1, 9)
	require.NoError(err)

	// Call with currentHeight == 10 (the expiry height of "at-boundary")
	// With <= semantics (L-07 fix), ballot at exactly the expiry height SHOULD be expired
	err = f.k.ExpireBallotsBeforeHeight(f.ctx, 10)
	require.NoError(err)

	// "at-boundary" has BlockHeightExpiry == 10, currentHeight == 10 → expired (<=)
	got, err := f.k.GetBallot(f.ctx, "at-boundary")
	require.NoError(err)
	require.Equal(types.BallotStatus_BALLOT_STATUS_EXPIRED, got.Status,
		"ballot at exactly the expiry height should be expired with <= semantics")

	// "below-boundary" has BlockHeightExpiry == 9, currentHeight == 10 → expired
	got2, err := f.k.GetBallot(f.ctx, "below-boundary")
	require.NoError(err)
	require.Equal(types.BallotStatus_BALLOT_STATUS_EXPIRED, got2.Status,
		"ballot below the expiry height should be expired")
}

func TestExpireBallotsBeforeHeight_NoOp(t *testing.T) {
	f := SetupTest(t)
	require := require.New(t)

	// Create 3 active ballots with long expiry (100 blocks)
	for i := 0; i < 3; i++ {
		_, err := f.k.CreateBallot(f.ctx, fmt.Sprintf("noop-%d", i),
			types.BallotObservationType_BALLOT_OBSERVATION_TYPE_INBOUND_TX,
			[]string{"v1"}, 1, 100)
		require.NoError(err)
	}

	// Call with a low currentHeight — none should expire
	err := f.k.ExpireBallotsBeforeHeight(f.ctx, 5)
	require.NoError(err)

	// Assert no ballots were expired
	for i := 0; i < 3; i++ {
		id := fmt.Sprintf("noop-%d", i)
		got, err := f.k.GetBallot(f.ctx, id)
		require.NoError(err)
		require.Equal(types.BallotStatus_BALLOT_STATUS_PENDING, got.Status, "ballot %s should still be pending", id)

		has, err := f.k.ActiveBallotIDs.Has(f.ctx, id)
		require.NoError(err)
		require.True(has, "ballot %s should still be in ActiveBallotIDs", id)
	}
}

func TestExpireBallotsBeforeHeight_EmptySet(t *testing.T) {
	f := SetupTest(t)
	require := require.New(t)

	// Call with no active ballots — should not error
	err := f.k.ExpireBallotsBeforeHeight(f.ctx, 100)
	require.NoError(err)
}
