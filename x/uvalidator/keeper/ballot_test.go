package keeper_test

import (
	"testing"

	"github.com/rollchains/pchain/x/uvalidator/types"
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

	// Create ballot that expires at +1
	b, err := f.k.CreateBallot(f.ctx, "b6", types.BallotObservationType_BALLOT_OBSERVATION_TYPE_INBOUND_TX,
		[]string{"v1"}, 1, 1)
	require.NoError(err)

	// Expire with currentHeight past expiry
	err = f.k.ExpireBallotsBeforeHeight(f.ctx, b.BlockHeightCreated+2)
	require.NoError(err)

	got, err := f.k.GetBallot(f.ctx, b.Id)
	require.NoError(err)
	require.Equal(types.BallotStatus_BALLOT_STATUS_EXPIRED, got.Status)

	// Expire with not yet expired height → no change
	b2, err := f.k.CreateBallot(f.ctx, "b7", types.BallotObservationType_BALLOT_OBSERVATION_TYPE_INBOUND_TX,
		[]string{"v1"}, 1, 10)
	require.NoError(err)

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
