package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/x/ucallback/types"
)

func newRead(id, txHash string, expiry uint64, status types.UniversalReadStatus) types.UniversalRead {
	return types.UniversalRead{
		Id:     id,
		Status: status,
		Request: &types.ReadRequest{
			RequestId:         id,
			DestinationChain:  "eip155:1",
			ExpiryBlockHeight: expiry,
			RequestedTxHash:   txHash,
		},
	}
}

func TestSetUniversalRead_RoundTrips(t *testing.T) {
	f := SetupTest(t)

	require.False(t, f.k.HasUniversalRead(f.ctx, "0xaaa"))

	ur := newRead("0xaaa", "0xTX", 100, types.UniversalReadStatus_UNIVERSAL_READ_STATUS_PENDING)
	require.NoError(t, f.k.SetUniversalRead(f.ctx, ur))

	require.True(t, f.k.HasUniversalRead(f.ctx, "0xaaa"))
	got, found := f.k.GetUniversalRead(f.ctx, "0xaaa")
	require.True(t, found)
	require.Equal(t, "0xaaa", got.Id)
	require.Equal(t, "eip155:1", got.Request.DestinationChain)

	_, found = f.k.GetUniversalRead(f.ctx, "0xmissing")
	require.False(t, found)
}

func TestSetUniversalRead_RejectsEmptyID(t *testing.T) {
	f := SetupTest(t)
	require.Error(t, f.k.SetUniversalRead(f.ctx, types.UniversalRead{}))
}

func collectDueBy(t *testing.T, f *testFixture, height uint64) []string {
	t.Helper()
	var got []string
	err := f.k.IterateExpiredBy(f.ctx, height, func(ur types.UniversalRead) bool {
		got = append(got, ur.Id)
		return true
	})
	require.NoError(t, err)
	return got
}

// The sweep is bounded by height and ordered ascending.
func TestIterateExpiredBy_RespectsHeight(t *testing.T) {
	f := SetupTest(t)

	require.NoError(t, f.k.SetUniversalRead(f.ctx,
		newRead("0xlow", "0xTX", 50, types.UniversalReadStatus_UNIVERSAL_READ_STATUS_PENDING)))
	require.NoError(t, f.k.SetUniversalRead(f.ctx,
		newRead("0xmid", "0xTX", 100, types.UniversalReadStatus_UNIVERSAL_READ_STATUS_VOTING)))
	require.NoError(t, f.k.SetUniversalRead(f.ctx,
		newRead("0xhigh", "0xTX", 150, types.UniversalReadStatus_UNIVERSAL_READ_STATUS_PENDING)))

	require.Equal(t, []string{"0xlow"}, collectDueBy(t, f, 50))
	require.Equal(t, []string{"0xlow", "0xmid"}, collectDueBy(t, f, 100), "ascending by expiry height")
	require.Equal(t, []string{"0xlow", "0xmid", "0xhigh"}, collectDueBy(t, f, 999))
}

// Settling a read removes it from the in-flight set; the record itself remains.
func TestSetUniversalRead_SettledLeavesInFlightSet(t *testing.T) {
	f := SetupTest(t)

	ur := newRead("0xaaa", "0xTX", 100, types.UniversalReadStatus_UNIVERSAL_READ_STATUS_PENDING)
	require.NoError(t, f.k.SetUniversalRead(f.ctx, ur))
	require.Equal(t, []string{"0xaaa"}, collectDueBy(t, f, 100))

	ur.Status = types.UniversalReadStatus_UNIVERSAL_READ_STATUS_FULFILLED
	require.NoError(t, f.k.SetUniversalRead(f.ctx, ur))

	require.Empty(t, collectDueBy(t, f, 999), "settled reads are not swept")
	require.True(t, f.k.HasUniversalRead(f.ctx, "0xaaa"), "the record survives")
}

func collectByTx(t *testing.T, f *testFixture, txHash string) []string {
	t.Helper()
	var got []string
	err := f.k.IterateReadsByTxHash(f.ctx, txHash, func(ur types.UniversalRead) bool {
		got = append(got, ur.Id)
		return true
	})
	require.NoError(t, err)
	return got
}

// One Push tx emitting several ReadRequested logs produces several independent
// records that are still reassemblable as a batch.
func TestSetUniversalRead_BatchedRequestsShareTxHash(t *testing.T) {
	f := SetupTest(t)

	for _, id := range []string{"0xaaa", "0xbbb", "0xccc"} {
		require.NoError(t, f.k.SetUniversalRead(f.ctx,
			newRead(id, "0xBATCH", 100, types.UniversalReadStatus_UNIVERSAL_READ_STATUS_PENDING)))
	}
	// a read from a different tx must not leak into the batch
	require.NoError(t, f.k.SetUniversalRead(f.ctx,
		newRead("0xddd", "0xOTHER", 100, types.UniversalReadStatus_UNIVERSAL_READ_STATUS_PENDING)))

	require.ElementsMatch(t, []string{"0xaaa", "0xbbb", "0xccc"}, collectByTx(t, f, "0xBATCH"))
	require.Equal(t, []string{"0xddd"}, collectByTx(t, f, "0xOTHER"))
}

// Siblings from one batch settle independently — one FULFILLED, one still pending.
func TestSetUniversalRead_BatchSiblingsSettleIndependently(t *testing.T) {
	f := SetupTest(t)

	require.NoError(t, f.k.SetUniversalRead(f.ctx,
		newRead("0xaaa", "0xBATCH", 100, types.UniversalReadStatus_UNIVERSAL_READ_STATUS_PENDING)))
	require.NoError(t, f.k.SetUniversalRead(f.ctx,
		newRead("0xbbb", "0xBATCH", 100, types.UniversalReadStatus_UNIVERSAL_READ_STATUS_PENDING)))

	// settle only one of them
	settled := newRead("0xaaa", "0xBATCH", 100, types.UniversalReadStatus_UNIVERSAL_READ_STATUS_FULFILLED)
	require.NoError(t, f.k.SetUniversalRead(f.ctx, settled))

	// the settled one drops out of the expiry sweep, its sibling does not
	require.Equal(t, []string{"0xbbb"}, collectDueBy(t, f, 100))
	// but both remain listed under the batch — reads-by-tx is provenance, not state
	require.ElementsMatch(t, []string{"0xaaa", "0xbbb"}, collectByTx(t, f, "0xBATCH"))
}

// Repointing a read's ballot key must not leave the old key resolvable.
func TestGetUniversalReadByBallot_Repointed(t *testing.T) {
	f := SetupTest(t)

	ur := newRead("0xaaa", "0xTX", 100, types.UniversalReadStatus_UNIVERSAL_READ_STATUS_PENDING)
	ur.BallotKey = "ballot-old"
	require.NoError(t, f.k.SetUniversalRead(f.ctx, ur))

	ur.BallotKey = "ballot-new"
	require.NoError(t, f.k.SetUniversalRead(f.ctx, ur))

	_, found := f.k.GetUniversalReadByBallot(f.ctx, "ballot-old")
	require.False(t, found, "the old ballot key must no longer resolve")

	byNew, found := f.k.GetUniversalReadByBallot(f.ctx, "ballot-new")
	require.True(t, found)
	require.Equal(t, "0xaaa", byNew.Id)
}

// The lookup scans the in-flight set, so a settled read is deliberately NOT
// findable by ballot. The ballot terminal hook must treat "not found" as
// "already handled", exactly as uexecutor's hook does.
func TestGetUniversalReadByBallot_SettledReadIsNotFound(t *testing.T) {
	f := SetupTest(t)

	ur := newRead("0xaaa", "0xTX", 100, types.UniversalReadStatus_UNIVERSAL_READ_STATUS_PENDING)
	ur.BallotKey = "ballot-1"
	require.NoError(t, f.k.SetUniversalRead(f.ctx, ur))

	_, found := f.k.GetUniversalReadByBallot(f.ctx, "ballot-1")
	require.True(t, found, "resolvable while in flight")

	ur.Status = types.UniversalReadStatus_UNIVERSAL_READ_STATUS_FULFILLED
	require.NoError(t, f.k.SetUniversalRead(f.ctx, ur))

	_, found = f.k.GetUniversalReadByBallot(f.ctx, "ballot-1")
	require.False(t, found, "settled reads leave the in-flight set")

	// the record itself is untouched — only the index dropped it
	got, ok := f.k.GetUniversalRead(f.ctx, "0xaaa")
	require.True(t, ok)
	require.Equal(t, "ballot-1", got.BallotKey)
}

// Only the read owning the ballot is returned, never a sibling sharing the scan.
func TestGetUniversalReadByBallot_PicksTheRightRead(t *testing.T) {
	f := SetupTest(t)

	for i, id := range []string{"0xaaa", "0xbbb", "0xccc"} {
		ur := newRead(id, "0xBATCH", uint64(100+i),
			types.UniversalReadStatus_UNIVERSAL_READ_STATUS_PENDING)
		ur.BallotKey = "ballot-" + id
		require.NoError(t, f.k.SetUniversalRead(f.ctx, ur))
	}

	got, found := f.k.GetUniversalReadByBallot(f.ctx, "ballot-0xbbb")
	require.True(t, found)
	require.Equal(t, "0xbbb", got.Id)

	_, found = f.k.GetUniversalReadByBallot(f.ctx, "ballot-unknown")
	require.False(t, found)
}

// Genesis round-trips records, and rebuilds every index from them.
func TestGenesis_RoundTripRebuildsIndexes(t *testing.T) {
	f := SetupTest(t)
	require.NoError(t, f.k.InitGenesis(f.ctx, types.DefaultGenesis()))

	pending := newRead("0xaaa", "0xTX", 100, types.UniversalReadStatus_UNIVERSAL_READ_STATUS_PENDING)
	pending.BallotKey = "ballot-1"
	require.NoError(t, f.k.SetUniversalRead(f.ctx, pending))
	require.NoError(t, f.k.SetUniversalRead(f.ctx,
		newRead("0xbbb", "0xTX", 100, types.UniversalReadStatus_UNIVERSAL_READ_STATUS_FULFILLED)))

	exported := f.k.ExportGenesis(f.ctx)
	require.Len(t, exported.UniversalReads, 2)

	// re-import into a clean fixture
	g := SetupTest(t)
	require.NoError(t, g.k.InitGenesis(g.ctx, exported))

	_, found := g.k.GetUniversalRead(g.ctx, "0xaaa")
	require.True(t, found)

	// indexes are rebuilt, not imported
	byBallot, found := g.k.GetUniversalReadByBallot(g.ctx, "ballot-1")
	require.True(t, found)
	require.Equal(t, "0xaaa", byBallot.Id)

	require.ElementsMatch(t, []string{"0xaaa", "0xbbb"}, collectByTx(t, g, "0xTX"))
	require.Equal(t, []string{"0xaaa"}, collectDueBy(t, g, 100),
		"only the unsettled read is pending after re-import")
}
