package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/x/ucallback/types"
)

func pendingIDs(t *testing.T, f *testFixture) []string {
	t.Helper()
	res, err := f.queryServer.AllPendingReadRequests(f.ctx,
		&types.QueryAllPendingReadRequestsRequest{})
	require.NoError(t, err)
	got := make([]string, 0, len(res.Reads))
	for _, r := range res.Reads {
		got = append(got, r.Id)
	}
	return got
}

// Only unsettled reads are listed.
func TestAllPendingReadRequests_ExcludesSettled(t *testing.T) {
	f := SetupTest(t)

	require.NoError(t, f.k.SetUniversalRead(f.ctx,
		newRead("0xpending", "0xTX", 500, types.UniversalReadStatus_UNIVERSAL_READ_STATUS_PENDING)))
	require.NoError(t, f.k.SetUniversalRead(f.ctx,
		newRead("0xvoting", "0xTX", 500, types.UniversalReadStatus_UNIVERSAL_READ_STATUS_VOTING)))
	require.NoError(t, f.k.SetUniversalRead(f.ctx,
		newRead("0xdone", "0xTX", 500, types.UniversalReadStatus_UNIVERSAL_READ_STATUS_FULFILLED)))

	require.ElementsMatch(t, []string{"0xpending", "0xvoting"}, pendingIDs(t, f))
}

// A read past its expiry height is withheld even though the sweeper has not run,
// so validators never pick up work that can no longer be fulfilled in time.
func TestAllPendingReadRequests_WithholdsExpired(t *testing.T) {
	f := SetupTest(t)

	f.ctx = f.ctx.WithBlockHeight(100)

	require.NoError(t, f.k.SetUniversalRead(f.ctx,
		newRead("0xexpired", "0xTX", 50, types.UniversalReadStatus_UNIVERSAL_READ_STATUS_PENDING)))
	require.NoError(t, f.k.SetUniversalRead(f.ctx,
		newRead("0xatheight", "0xTX", 100, types.UniversalReadStatus_UNIVERSAL_READ_STATUS_PENDING)))
	require.NoError(t, f.k.SetUniversalRead(f.ctx,
		newRead("0xlive", "0xTX", 150, types.UniversalReadStatus_UNIVERSAL_READ_STATUS_PENDING)))

	// still unsettled in state — the filter is at read time, not a mutation
	require.True(t, f.k.HasUniversalRead(f.ctx, "0xexpired"))

	require.Equal(t, []string{"0xlive"}, pendingIDs(t, f),
		"expiry height is exclusive: a read expiring at the current height is already too late")
}

func TestAllPendingReadRequests_Empty(t *testing.T) {
	f := SetupTest(t)
	require.Empty(t, pendingIDs(t, f))
}

func TestAllPendingReadRequests_NilRequest(t *testing.T) {
	f := SetupTest(t)
	_, err := f.queryServer.AllPendingReadRequests(f.ctx, nil)
	require.Error(t, err)
}

// A read is served at any lifecycle stage — this endpoint answers "what happened
// to my request", so unlike the pending list it must not filter.
func TestUniversalRead_ServesSettledAndExpired(t *testing.T) {
	f := SetupTest(t)
	f.ctx = f.ctx.WithBlockHeight(100)

	for id, st := range map[string]types.UniversalReadStatus{
		"0xpending": types.UniversalReadStatus_UNIVERSAL_READ_STATUS_PENDING,
		"0xdone":    types.UniversalReadStatus_UNIVERSAL_READ_STATUS_FULFILLED,
		"0xgone":    types.UniversalReadStatus_UNIVERSAL_READ_STATUS_EXPIRED,
	} {
		require.NoError(t, f.k.SetUniversalRead(f.ctx, newRead(id, "0xTX", 50, st)))
	}

	for _, id := range []string{"0xpending", "0xdone", "0xgone"} {
		res, err := f.queryServer.UniversalRead(f.ctx,
			&types.QueryUniversalReadRequest{RequestId: id})
		require.NoError(t, err, id)
		require.Equal(t, id, res.Read.Id)
	}

	// ...even though only one of them is visible to validators
	require.Empty(t, pendingIDs(t, f))
}

func TestUniversalRead_NotFound(t *testing.T) {
	f := SetupTest(t)

	_, err := f.queryServer.UniversalRead(f.ctx,
		&types.QueryUniversalReadRequest{RequestId: "0xmissing"})
	require.Error(t, err)

	_, err = f.queryServer.UniversalRead(f.ctx, &types.QueryUniversalReadRequest{})
	require.Error(t, err, "empty request_id is rejected, not treated as not-found")
}

// The batch view returns siblings regardless of how each one settled.
func TestReadsByTx_ReturnsWholeBatch(t *testing.T) {
	f := SetupTest(t)

	require.NoError(t, f.k.SetUniversalRead(f.ctx,
		newRead("0xaaa", "0xBATCH", 500, types.UniversalReadStatus_UNIVERSAL_READ_STATUS_PENDING)))
	require.NoError(t, f.k.SetUniversalRead(f.ctx,
		newRead("0xbbb", "0xBATCH", 500, types.UniversalReadStatus_UNIVERSAL_READ_STATUS_FULFILLED)))
	require.NoError(t, f.k.SetUniversalRead(f.ctx,
		newRead("0xccc", "0xOTHER", 500, types.UniversalReadStatus_UNIVERSAL_READ_STATUS_PENDING)))

	res, err := f.queryServer.ReadsByTx(f.ctx, &types.QueryReadsByTxRequest{TxHash: "0xBATCH"})
	require.NoError(t, err)
	ids := []string{}
	for _, r := range res.Reads {
		ids = append(ids, r.Id)
	}
	require.ElementsMatch(t, []string{"0xaaa", "0xbbb"}, ids)

	// an unknown tx is an empty batch, not an error
	res, err = f.queryServer.ReadsByTx(f.ctx, &types.QueryReadsByTxRequest{TxHash: "0xNOPE"})
	require.NoError(t, err)
	require.Empty(t, res.Reads)

	_, err = f.queryServer.ReadsByTx(f.ctx, &types.QueryReadsByTxRequest{})
	require.Error(t, err)
}
