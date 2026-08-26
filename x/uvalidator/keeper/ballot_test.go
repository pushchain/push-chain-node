package keeper_test

import (
	"fmt"
	"testing"

	"cosmossdk.io/collections"
	"github.com/pushchain/push-chain-node/x/uvalidator/keeper"
	"github.com/pushchain/push-chain-node/x/uvalidator/types"
	"github.com/stretchr/testify/require"
)

const inboundBallot = types.BallotObservationType_BALLOT_OBSERVATION_TYPE_INBOUND_TX

// pendingIndexed reports whether the (expiryHeight, ballotID) row exists in the
// PendingByExpiry index.
func pendingIndexed(t *testing.T, f *testFixture, id string, expiryHeight int64) bool {
	t.Helper()
	has, err := f.k.PendingByExpiry.Has(f.ctx, collections.Join(expiryHeight, id))
	require.NoError(t, err)
	return has
}

// pendingIndexIDs returns every ballot ID currently carried by the expiry index.
func pendingIndexIDs(t *testing.T, f *testFixture) []string {
	t.Helper()
	ids := []string{}
	require.NoError(t, f.k.PendingByExpiry.Walk(f.ctx, nil,
		func(key collections.Pair[int64, string]) (bool, error) {
			ids = append(ids, key.K2())
			return false, nil
		}))
	return ids
}

// expiredCount returns how many ballots sit in ExpiredBallotIDs.
func expiredCount(t *testing.T, f *testFixture) int {
	t.Helper()
	n := 0
	require.NoError(t, f.k.ExpiredBallotIDs.Walk(f.ctx, nil, func(string) (bool, error) {
		n++
		return false, nil
	}))
	return n
}

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

// Creation must no longer sweep expired ballots: that scan walked the whole
// active set on a consensus hot path. Expiry moved to the module EndBlocker.
func TestCreateBallot_DoesNotExpireOnCreate(t *testing.T) {
	f := SetupTest(t)
	require := require.New(t)

	// Create a ballot that comes due quickly (expiry = 1 block)
	oldBallot, err := f.k.CreateBallot(f.ctx, "old",
		types.BallotObservationType_BALLOT_OBSERVATION_TYPE_INBOUND_TX,
		[]string{"v1"}, 1, 1)
	require.NoError(err)

	// Advance well past the old ballot's expiry height
	f.ctx = f.ctx.WithBlockHeight(oldBallot.BlockHeightCreated + 5)

	// Creating a NEW ballot must NOT expire the due one — no scan on create.
	newBallot, err := f.k.CreateBallot(f.ctx, "new",
		types.BallotObservationType_BALLOT_OBSERVATION_TYPE_INBOUND_TX,
		[]string{"v2"}, 1, 10)
	require.NoError(err)
	require.Equal("new", newBallot.Id)

	got, err := f.k.GetBallot(f.ctx, "old")
	require.NoError(err)
	require.Equal(types.BallotStatus_BALLOT_STATUS_PENDING, got.Status,
		"CreateBallot must not expire ballots; expiry belongs to the EndBlocker sweep")

	has, err := f.k.ActiveBallotIDs.Has(f.ctx, "old")
	require.NoError(err)
	require.True(has, "due ballot must stay active until the sweep runs")

	// The sweep — not creation — is what expires it.
	require.NoError(f.k.ExpireBallotsBeforeHeight(f.ctx, f.ctx.BlockHeight()))

	got, err = f.k.GetBallot(f.ctx, "old")
	require.NoError(err)
	require.Equal(types.BallotStatus_BALLOT_STATUS_EXPIRED, got.Status)

	// The not-yet-due ballot survives the sweep.
	gotNew, err := f.k.GetBallot(f.ctx, "new")
	require.NoError(err)
	require.Equal(types.BallotStatus_BALLOT_STATUS_PENDING, gotNew.Status)
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

func TestCreateBallot_DoesNotExpireMultipleOldOnCreate(t *testing.T) {
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

	// Create fresh ballot — must NOT trigger any cleanup
	_, err = f.k.CreateBallot(f.ctx, "fresh",
		types.BallotObservationType_BALLOT_OBSERVATION_TYPE_INBOUND_TX,
		[]string{"v3"}, 1, 5)
	require.NoError(err)

	// Both due ballots must still be pending — creation does not sweep
	got1, err := f.k.GetBallot(f.ctx, b1.Id)
	require.NoError(err)
	require.Equal(types.BallotStatus_BALLOT_STATUS_PENDING, got1.Status)

	got2, err := f.k.GetBallot(f.ctx, b2.Id)
	require.NoError(err)
	require.Equal(types.BallotStatus_BALLOT_STATUS_PENDING, got2.Status)

	// The sweep expires them.
	require.NoError(f.k.ExpireBallotsBeforeHeight(f.ctx, f.ctx.BlockHeight()))

	got1, err = f.k.GetBallot(f.ctx, b1.Id)
	require.NoError(err)
	require.Equal(types.BallotStatus_BALLOT_STATUS_EXPIRED, got1.Status)

	got2, err = f.k.GetBallot(f.ctx, b2.Id)
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

// ─── PendingByExpiry index ───────────────────────────────────────────────────

// TestPendingByExpiryIndex_MirrorsEveryActiveSetWriter is the guard against a
// missed mirror site. Every writer of ActiveBallotIDs must write PendingByExpiry
// too; a miss makes the index drift from reality, which either hides a ballot
// from the sweep forever or lets the sweep act on a ballot that is no longer
// active. Each subtest asserts the index state FIRST, so the assertion that
// catches the missing mirror runs before anything else can abort the subtest.
func TestPendingByExpiryIndex_MirrorsEveryActiveSetWriter(t *testing.T) {
	t.Run("CreateBallot indexes under the expiry height", func(t *testing.T) {
		f := SetupTest(t)

		b, err := f.k.CreateBallot(f.ctx, "idx-create", inboundBallot, []string{"v1"}, 1, 7)
		require.NoError(t, err)

		require.True(t, pendingIndexed(t, f, "idx-create", b.BlockHeightExpiry),
			"CreateBallot must mirror ActiveBallotIDs into PendingByExpiry")
		require.Equal(t, []string{"idx-create"}, pendingIndexIDs(t, f))

		// The row must be keyed by the expiry height, not the creation height —
		// that is the whole point of the index.
		require.NotEqual(t, b.BlockHeightCreated, b.BlockHeightExpiry)
		require.False(t, pendingIndexed(t, f, "idx-create", b.BlockHeightCreated),
			"index must be keyed by expiry height, not creation height")
	})

	t.Run("MarkBallotExpired unindexes", func(t *testing.T) {
		f := SetupTest(t)

		b, err := f.k.CreateBallot(f.ctx, "idx-expire", inboundBallot, []string{"v1"}, 1, 3)
		require.NoError(t, err)
		require.NoError(t, f.k.MarkBallotExpired(f.ctx, b.Id))

		require.False(t, pendingIndexed(t, f, b.Id, b.BlockHeightExpiry),
			"MarkBallotExpired must remove the PendingByExpiry row")
		require.Empty(t, pendingIndexIDs(t, f))

		// A leaked row would be re-processed by the sweep every single block,
		// permanently consuming part of the per-block budget.
		require.NoError(t, f.k.ExpireBallotsBeforeHeight(f.ctx, b.BlockHeightExpiry+1))
		require.Empty(t, pendingIndexIDs(t, f))
	})

	t.Run("MarkBallotFinalized unindexes", func(t *testing.T) {
		f := SetupTest(t)

		b, err := f.k.CreateBallot(f.ctx, "idx-final", inboundBallot, []string{"v1"}, 1, 3)
		require.NoError(t, err)
		require.NoError(t, f.k.MarkBallotFinalized(f.ctx, b.Id, types.BallotStatus_BALLOT_STATUS_PASSED))

		require.False(t, pendingIndexed(t, f, b.Id, b.BlockHeightExpiry),
			"MarkBallotFinalized must remove the PendingByExpiry row")

		// Behavioural consequence of a leaked row: the sweep would reach a
		// finalized ballot and overwrite PASSED with EXPIRED.
		require.NoError(t, f.k.ExpireBallotsBeforeHeight(f.ctx, b.BlockHeightExpiry+1))
		got, err := f.k.GetBallot(f.ctx, b.Id)
		require.NoError(t, err)
		require.Equal(t, types.BallotStatus_BALLOT_STATUS_PASSED, got.Status,
			"a stale index row let the sweep overwrite a finalized ballot")
	})

	t.Run("DeleteBallot unindexes", func(t *testing.T) {
		f := SetupTest(t)

		b, err := f.k.CreateBallot(f.ctx, "idx-delete", inboundBallot, []string{"v1"}, 1, 3)
		require.NoError(t, err)
		require.NoError(t, f.k.DeleteBallot(f.ctx, b.Id))

		require.False(t, pendingIndexed(t, f, b.Id, b.BlockHeightExpiry),
			"DeleteBallot must remove the PendingByExpiry row")
		require.Empty(t, pendingIndexIDs(t, f))

		// Deleting an absent ballot stays a no-op.
		require.NoError(t, f.k.DeleteBallot(f.ctx, b.Id))
		require.Empty(t, pendingIndexIDs(t, f))
	})

	t.Run("VoteOnBallot create path indexes the new ballot", func(t *testing.T) {
		f := SetupTest(t)

		b, _, isNew, err := f.k.VoteOnBallot(f.ctx, "idx-vote", inboundBallot,
			"v1", types.VoteResult_VOTE_RESULT_SUCCESS,
			[]string{"v1", "v2"}, 2, 9)
		require.NoError(t, err)
		require.True(t, isNew)

		require.True(t, pendingIndexed(t, f, "idx-vote", b.BlockHeightExpiry),
			"the vote-driven create path must leave the expiry index populated")
	})
}

// TestExpireBallotsBeforeHeight_OnlyDueRowsAreTouched pins the range semantics:
// rows past currentHeight are neither expired nor dropped from the index.
func TestExpireBallotsBeforeHeight_OnlyDueRowsAreTouched(t *testing.T) {
	f := SetupTest(t)
	require := require.New(t)

	// Created at height 0 → expiry heights 3, 10 and exactly 5.
	due, err := f.k.CreateBallot(f.ctx, "due", inboundBallot, []string{"v1"}, 1, 3)
	require.NoError(err)
	atBoundary, err := f.k.CreateBallot(f.ctx, "at-boundary", inboundBallot, []string{"v1"}, 1, 5)
	require.NoError(err)
	future, err := f.k.CreateBallot(f.ctx, "future", inboundBallot, []string{"v1"}, 1, 10)
	require.NoError(err)

	require.NoError(f.k.ExpireBallotsBeforeHeight(f.ctx, 5))

	// The not-yet-due row must survive in the index for a later block.
	require.Equal([]string{"future"}, pendingIndexIDs(t, f))
	require.True(pendingIndexed(t, f, future.Id, future.BlockHeightExpiry))

	gotFuture, err := f.k.GetBallot(f.ctx, future.Id)
	require.NoError(err)
	require.Equal(types.BallotStatus_BALLOT_STATUS_PENDING, gotFuture.Status)

	// Expiry is inclusive of currentHeight (`<=` semantics).
	for _, id := range []string{due.Id, atBoundary.Id} {
		got, err := f.k.GetBallot(f.ctx, id)
		require.NoError(err)
		require.Equal(types.BallotStatus_BALLOT_STATUS_EXPIRED, got.Status, "ballot %s should be expired", id)
	}
}

// TestExpireBallotsBeforeHeight_CapsAtMaxExpiriesPerBlock proves the per-block
// bound holds and that the leftovers are carried in the index to the next block.
func TestExpireBallotsBeforeHeight_CapsAtMaxExpiriesPerBlock(t *testing.T) {
	f := SetupTest(t)
	require := require.New(t)

	const extra = 7
	total := keeper.MaxExpiriesPerBlock + extra

	// All due at height 1, i.e. the whole backlog comes due at once.
	for i := 0; i < total; i++ {
		_, err := f.k.CreateBallot(f.ctx, fmt.Sprintf("cap-%03d", i), inboundBallot, []string{"v1"}, 1, 1)
		require.NoError(err)
	}
	require.Len(pendingIndexIDs(t, f), total)

	// First sweep: exactly MaxExpiriesPerBlock, never the whole backlog.
	require.NoError(f.k.ExpireBallotsBeforeHeight(f.ctx, 100))
	require.Equal(keeper.MaxExpiriesPerBlock, expiredCount(t, f),
		"a single sweep must expire at most MaxExpiriesPerBlock ballots")
	require.Len(pendingIndexIDs(t, f), extra,
		"the remainder must stay in the index for the next block")

	// Second sweep drains the rest.
	require.NoError(f.k.ExpireBallotsBeforeHeight(f.ctx, 100))
	require.Equal(total, expiredCount(t, f))
	require.Empty(pendingIndexIDs(t, f))
}

// TestExpireBallotsBeforeHeight_OrphanedIndexRow covers the defensive path: an
// index row whose ballot record is gone must be dropped instead of wedging the
// sweep on every subsequent block.
func TestExpireBallotsBeforeHeight_OrphanedIndexRow(t *testing.T) {
	f := SetupTest(t)
	require := require.New(t)

	b, err := f.k.CreateBallot(f.ctx, "orphan", inboundBallot, []string{"v1"}, 1, 1)
	require.NoError(err)

	// Drop the record behind the index row's back.
	require.NoError(f.k.Ballots.Remove(f.ctx, b.Id))
	require.True(pendingIndexed(t, f, b.Id, b.BlockHeightExpiry))

	// A healthy ballot queued behind the orphan must still get expired.
	good, err := f.k.CreateBallot(f.ctx, "good", inboundBallot, []string{"v1"}, 1, 2)
	require.NoError(err)

	require.NoError(f.k.ExpireBallotsBeforeHeight(f.ctx, 100))

	require.Empty(pendingIndexIDs(t, f), "the orphaned row must be dropped, not retried forever")
	gotGood, err := f.k.GetBallot(f.ctx, good.Id)
	require.NoError(err)
	require.Equal(types.BallotStatus_BALLOT_STATUS_EXPIRED, gotGood.Status)
}
