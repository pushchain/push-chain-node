package keeper_test

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/x/ucallback/keeper"
	"github.com/pushchain/push-chain-node/x/ucallback/types"
	uvalidatortypes "github.com/pushchain/push-chain-node/x/uvalidator/types"
)

// expiredIDs returns the request ids the sweeper called the contract for.
func expiredIDs(f *testFixture) []string {
	var got []string
	for _, c := range f.evm.calls {
		if c.method == types.MethodExpireExternalRead {
			got = append(got, fmt.Sprintf("0x%x", c.args[0].(*big.Int)))
		}
	}
	return got
}

// Only requests past their deadline are retired, and the boundary is exclusive —
// matching the query filter and the vote guard.
func TestSweepExpired_RetiresOnlyOverdue(t *testing.T) {
	f := SetupTest(t)
	f.ctx = f.ctx.WithBlockHeight(100)

	seedRead(t, f, "0x50", 50)  // overdue
	seedRead(t, f, "0x64", 100) // exactly at the deadline — already too late
	seedRead(t, f, "0xc8", 200) // still live

	require.NoError(t, f.k.SweepExpired(f.ctx))

	require.ElementsMatch(t, []string{"0x50", "0x64"}, expiredIDs(f))

	for _, id := range []string{"0x50", "0x64"} {
		ur, _ := f.k.GetUniversalRead(f.ctx, id)
		require.Equal(t, types.UniversalReadStatus_UNIVERSAL_READ_STATUS_EXPIRED, ur.Status, id)
		require.Len(t, ur.PcTx, 1, id)
	}
	live, _ := f.k.GetUniversalRead(f.ctx, "0xc8")
	require.Equal(t, types.UniversalReadStatus_UNIVERSAL_READ_STATUS_PENDING, live.Status)
	require.Equal(t, []string{"0xc8"}, pendingIDs(t, f))
}

// A second sweep must not touch what the first already retired.
func TestSweepExpired_IsIdempotent(t *testing.T) {
	f := SetupTest(t)
	f.ctx = f.ctx.WithBlockHeight(100)
	seedRead(t, f, "0xaa", 50)

	require.NoError(t, f.k.SweepExpired(f.ctx))
	require.Len(t, f.evm.calls, 1)

	require.NoError(t, f.k.SweepExpired(f.ctx))
	require.NoError(t, f.k.SweepExpired(f.ctx))
	require.Len(t, f.evm.calls, 1, "a retired request must never be swept again")

	ur, _ := f.k.GetUniversalRead(f.ctx, "0xaa")
	require.Len(t, ur.PcTx, 1)
}

// A request that reached quorum is gone from the in-flight set, so the sweeper can
// never expire something already fulfilled — even past its deadline.
func TestSweepExpired_SkipsFulfilled(t *testing.T) {
	f := SetupTest(t)
	f.ctx = f.ctx.WithBlockHeight(10)

	seedRead(t, f, "0xaa", 50)
	key := voteToQuorum(t, f, "0xaa", obs(0x01))
	require.NoError(t, fireTerminal(f, key, uvalidatortypes.BallotStatus_BALLOT_STATUS_PASSED))
	require.Len(t, f.evm.calls, 1, "fulfilled")

	// now well past the deadline
	f.ctx = f.ctx.WithBlockHeight(999)
	require.NoError(t, f.k.SweepExpired(f.ctx))

	require.Empty(t, expiredIDs(f), "must not expire a fulfilled request")
	ur, _ := f.k.GetUniversalRead(f.ctx, "0xaa")
	require.Equal(t, types.UniversalReadStatus_UNIVERSAL_READ_STATUS_FULFILLED, ur.Status)
}

// A request validators diverged on has several ballots but is one entry in the
// in-flight set, so it is retired once — expiry credits the funder a refund
// (UniversalCallback._settle), so a second call would not be merely redundant.
func TestSweepExpired_DivergedRequestRetiredOnce(t *testing.T) {
	f := SetupTest(t)
	f.ctx = f.ctx.WithBlockHeight(10)
	v := seedVoters(t, f, 4)
	seedRead(t, f, "0xaa", 50)

	_, err := f.k.VoteReadResult(f.ctx, v[0], "0xaa", obs(0x01))
	require.NoError(t, err)
	_, err = f.k.VoteReadResult(f.ctx, v[1], "0xaa", obs(0x02))
	require.NoError(t, err)
	require.Equal(t, 2, f.uvalidator.ballotCount(), "validators diverged")

	f.ctx = f.ctx.WithBlockHeight(100)
	require.NoError(t, f.k.SweepExpired(f.ctx))

	require.Equal(t, []string{"0xaa"}, expiredIDs(f), "one request, one expiry")
	ur, _ := f.k.GetUniversalRead(f.ctx, "0xaa")
	require.Len(t, ur.PcTx, 1)
}

// A failed call leaves the request in flight so the next block retries it.
// Expiry credits the funder a refund and only this module may call it, so treating
// a transient failure as final would strand that money for good.
func TestSweepExpired_RetriesTransientFailure(t *testing.T) {
	f := SetupTest(t)
	f.ctx = f.ctx.WithBlockHeight(100)
	f.evm.vmErrors = []string{"execution reverted"} // first attempt only
	seedRead(t, f, "0xaa", 50)

	require.NoError(t, f.k.SweepExpired(f.ctx))

	ur, _ := f.k.GetUniversalRead(f.ctx, "0xaa")
	require.Equal(t, types.UniversalReadStatus_UNIVERSAL_READ_STATUS_PENDING, ur.Status,
		"not retired — the contract never acknowledged it")
	require.Len(t, ur.PcTx, 1)
	require.Equal(t, "FAILED", ur.PcTx[0].Status)
	// NB: not pendingIDs — that query withholds anything past its deadline, so it
	// is empty either way. The sweeper's own view is what matters here.
	require.Equal(t, []string{"0xaa"}, collectDueBy(t, f, 100),
		"still in the in-flight set for the next sweep")

	// next block succeeds
	require.NoError(t, f.k.SweepExpired(f.ctx))

	ur, _ = f.k.GetUniversalRead(f.ctx, "0xaa")
	require.Equal(t, types.UniversalReadStatus_UNIVERSAL_READ_STATUS_EXPIRED, ur.Status)
	require.Len(t, ur.PcTx, 2, "both attempts recorded")
	require.Equal(t, "SUCCESS", ur.PcTx[1].Status)
	require.Empty(t, collectDueBy(t, f, 100))
}

// Retries are bounded, and giving up is recorded as ABORTED rather than EXPIRED:
// the contract never acknowledged the request, so claiming it expired would assert
// a refund that was never credited.
func TestSweepExpired_GivesUpAfterMaxAttempts(t *testing.T) {
	f := SetupTest(t)
	f.ctx = f.ctx.WithBlockHeight(100)
	f.evm.vmErrors = make([]string, keeper.MaxExpiryAttempts)
	for i := range f.evm.vmErrors {
		f.evm.vmErrors[i] = "RequestAlreadyFulfilled"
	}
	seedRead(t, f, "0xaa", 50)

	for i := 1; i < keeper.MaxExpiryAttempts; i++ {
		require.NoError(t, f.k.SweepExpired(f.ctx))
		ur, _ := f.k.GetUniversalRead(f.ctx, "0xaa")
		require.Equal(t, types.UniversalReadStatus_UNIVERSAL_READ_STATUS_PENDING, ur.Status,
			"attempt %d must still retry", i)
	}

	// the last permitted attempt retires it
	require.NoError(t, f.k.SweepExpired(f.ctx))

	ur, _ := f.k.GetUniversalRead(f.ctx, "0xaa")
	require.Equal(t, types.UniversalReadStatus_UNIVERSAL_READ_STATUS_ABORTED, ur.Status,
		"not EXPIRED — the contract never accepted it")
	require.Contains(t, ur.ErrorMsg, "RequestAlreadyFulfilled",
		"the reason is on the record, not only in the logs")
	require.Len(t, ur.PcTx, keeper.MaxExpiryAttempts, "every attempt is on the record")
	require.Empty(t, collectDueBy(t, f, 100), "left the in-flight set")

	// and it stops consuming sweep slots
	before := len(f.evm.calls)
	require.NoError(t, f.k.SweepExpired(f.ctx))
	require.Equal(t, before, len(f.evm.calls), "no further attempts")
}

// The per-block bound caps work, and the backlog drains over later blocks —
// oldest first, since the set is ordered by deadline.
func TestSweepExpired_BoundedAndDrains(t *testing.T) {
	f := SetupTest(t)
	f.ctx = f.ctx.WithBlockHeight(100_000)

	total := keeper.MaxExpiriesPerBlock + 7
	for i := 0; i < total; i++ {
		seedRead(t, f, fmt.Sprintf("0x%x", 0x1000+i), uint64(10+i))
	}

	require.NoError(t, f.k.SweepExpired(f.ctx))
	require.Len(t, f.evm.calls, keeper.MaxExpiriesPerBlock, "capped")

	// the oldest deadlines went first
	require.Equal(t, fmt.Sprintf("0x%x", 0x1000), expiredIDs(f)[0])

	require.NoError(t, f.k.SweepExpired(f.ctx))
	require.Len(t, f.evm.calls, total, "backlog drains on the next block")

	require.Empty(t, collectDueBy(t, f, 100_000))
}

func TestSweepExpired_NothingDue(t *testing.T) {
	f := SetupTest(t)
	f.ctx = f.ctx.WithBlockHeight(10)
	seedRead(t, f, "0xaa", 500)

	require.NoError(t, f.k.SweepExpired(f.ctx))
	require.Empty(t, f.evm.calls)
	require.Equal(t, []string{"0xaa"}, pendingIDs(t, f))
}

func TestSweepExpired_EmptyState(t *testing.T) {
	f := SetupTest(t)
	f.ctx = f.ctx.WithBlockHeight(100)
	require.NoError(t, f.k.SweepExpired(f.ctx))
	require.Empty(t, f.evm.calls)
}

// Retiring uses the module account and advances its nonce, same as fulfilment.
func TestSweepExpired_UsesModuleNonce(t *testing.T) {
	f := SetupTest(t)
	f.ctx = f.ctx.WithBlockHeight(100)
	require.NoError(t, f.k.ModuleAccountNonce.Set(f.ctx, 5))

	seedRead(t, f, "0xaa", 10)
	seedRead(t, f, "0xbb", 20)
	require.NoError(t, f.k.SweepExpired(f.ctx))

	require.Equal(t, uint64(5), *f.evm.calls[0].nonce)
	require.Equal(t, uint64(6), *f.evm.calls[1].nonce, "each call advances it")
	require.Equal(t, moduleEVMAddr(), f.evm.calls[0].from)

	n, err := f.k.GetModuleAccountNonce(f.ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(7), n)
}

// ABORTED is terminal: it must leave the in-flight set and never be swept again,
// even though the contract may still consider the request pending.
func TestSweepExpired_AbortedIsTerminal(t *testing.T) {
	f := SetupTest(t)
	f.ctx = f.ctx.WithBlockHeight(100)
	f.evm.vmErrors = make([]string, keeper.MaxExpiryAttempts)
	for i := range f.evm.vmErrors {
		f.evm.vmErrors[i] = "boom"
	}
	seedRead(t, f, "0xaa", 50)

	for i := 0; i < keeper.MaxExpiryAttempts; i++ {
		require.NoError(t, f.k.SweepExpired(f.ctx))
	}

	ur, _ := f.k.GetUniversalRead(f.ctx, "0xaa")
	require.Equal(t, types.UniversalReadStatus_UNIVERSAL_READ_STATUS_ABORTED, ur.Status)

	// gone from every in-flight view
	require.Empty(t, collectDueBy(t, f, 999))
	require.Empty(t, pendingIDs(t, f))
	_, found := f.k.GetUniversalReadByBallot(f.ctx, ur.BallotKey)
	require.False(t, found)

	// but still queryable — this is the state an operator has to find
	res, err := f.queryServer.UniversalRead(f.ctx,
		&types.QueryUniversalReadRequest{RequestId: "0xaa"})
	require.NoError(t, err)
	require.Equal(t, types.UniversalReadStatus_UNIVERSAL_READ_STATUS_ABORTED, res.Read.Status)
	require.Contains(t, res.Read.ErrorMsg, "boom")
}

// A clean fulfilment leaves no error text behind — ErrorMsg is only ever set on a
// path that failed, so its presence is meaningful.
func TestFulfil_SuccessLeavesNoErrorMsg(t *testing.T) {
	f := SetupTest(t)
	f.ctx = f.ctx.WithBlockHeight(10)

	seedRead(t, f, "0xaa", 500)
	key := voteToQuorum(t, f, "0xaa", obs(0x01))
	require.NoError(t, fireTerminal(f, key, uvalidatortypes.BallotStatus_BALLOT_STATUS_PASSED))

	ur, _ := f.k.GetUniversalRead(f.ctx, "0xaa")
	require.Equal(t, types.UniversalReadStatus_UNIVERSAL_READ_STATUS_FULFILLED, ur.Status)
	require.Empty(t, ur.ErrorMsg)
	require.Equal(t, "SUCCESS", ur.PcTx[0].Status)
}

// abortedIDs lists what the operator-facing query returns.
func abortedIDs(t *testing.T, f *testFixture) []string {
	t.Helper()
	res, err := f.queryServer.AllAbortedReadRequests(f.ctx,
		&types.QueryAllAbortedReadRequestsRequest{})
	require.NoError(t, err)
	got := make([]string, 0, len(res.Reads))
	for _, r := range res.Reads {
		got = append(got, r.Id)
	}
	return got
}

// abandon drives one request all the way to ABORTED.
func abandon(t *testing.T, f *testFixture, id string, reason string) {
	t.Helper()
	seedRead(t, f, id, 50)
	for i := 0; i < keeper.MaxExpiryAttempts; i++ {
		f.evm.vmErrors = append(f.evm.vmErrors, reason)
		require.NoError(t, f.k.SweepExpired(f.ctx))
	}
	ur, _ := f.k.GetUniversalRead(f.ctx, id)
	require.Equal(t, types.UniversalReadStatus_UNIVERSAL_READ_STATUS_ABORTED, ur.Status)
}

// Only abandoned reads appear, and they carry the reason.
func TestAllAbortedReadRequests(t *testing.T) {
	f := SetupTest(t)
	f.ctx = f.ctx.WithBlockHeight(100)

	require.Empty(t, abortedIDs(t, f), "nothing abandoned yet")

	abandon(t, f, "0xaa", "RequestNotYetExpired")

	// a healthy expiry and a live request must not show up
	seedRead(t, f, "0xbb", 50)
	require.NoError(t, f.k.SweepExpired(f.ctx))
	seedRead(t, f, "0xcc", 9000)

	require.Equal(t, []string{"0xaa"}, abortedIDs(t, f))

	res, err := f.queryServer.AllAbortedReadRequests(f.ctx,
		&types.QueryAllAbortedReadRequestsRequest{})
	require.NoError(t, err)
	require.Contains(t, res.Reads[0].ErrorMsg, "RequestNotYetExpired",
		"the operator needs the reason, not just the id")
	require.Len(t, res.Reads[0].PcTx, keeper.MaxExpiryAttempts)

	// the healthy one settled cleanly
	bb, _ := f.k.GetUniversalRead(f.ctx, "0xbb")
	require.Equal(t, types.UniversalReadStatus_UNIVERSAL_READ_STATUS_EXPIRED, bb.Status)
}

// A read that leaves ABORTED — an admin retry that finally lands — drops off the
// list, so the query always reflects what still needs attention.
func TestAllAbortedReadRequests_ClearsOnRecovery(t *testing.T) {
	f := SetupTest(t)
	f.ctx = f.ctx.WithBlockHeight(100)

	abandon(t, f, "0xaa", "boom")
	require.Equal(t, []string{"0xaa"}, abortedIDs(t, f))

	// recovery: the request finally settles
	ur, _ := f.k.GetUniversalRead(f.ctx, "0xaa")
	ur.Status = types.UniversalReadStatus_UNIVERSAL_READ_STATUS_EXPIRED
	require.NoError(t, f.k.SetUniversalRead(f.ctx, ur))

	require.Empty(t, abortedIDs(t, f), "no longer needs intervention")
}

func TestAllAbortedReadRequests_NilRequest(t *testing.T) {
	f := SetupTest(t)
	_, err := f.queryServer.AllAbortedReadRequests(f.ctx, nil)
	require.Error(t, err)
}

// The retry budget must not be shortened by a failed fulfilment. A fulfil that did
// not settle leaves its PCTx behind and the read in flight, so counting PcTx
// entries would hand exactly those reads fewer expiry attempts than a clean one.
func TestSweepExpired_RetryBudgetIgnoresFulfilAttempts(t *testing.T) {
	f := SetupTest(t)
	f.ctx = f.ctx.WithBlockHeight(10)

	seedRead(t, f, "0xaa", 50)
	key := voteToQuorum(t, f, "0xaa", obs(0x01))

	// a fulfilment that reverts without settling — leaves a PCTx, stays in flight
	f.evm.vmErrors = []string{"execution reverted"}
	f.evm.revertData = selector("CallerIsNotUCallbackModule()")
	require.NoError(t, fireTerminal(f, key, uvalidatortypes.BallotStatus_BALLOT_STATUS_PASSED))

	ur, _ := f.k.GetUniversalRead(f.ctx, "0xaa")
	require.Len(t, ur.PcTx, 1, "the failed fulfil is on the record")
	require.Equal(t, uint32(0), ur.ExpiryAttempts, "but it is not an expiry attempt")

	// now the sweeper takes over, and must get its full budget
	f.ctx = f.ctx.WithBlockHeight(100)
	f.evm.revertData = nil
	for i := 1; i <= keeper.MaxExpiryAttempts; i++ {
		f.evm.vmErrors = []string{"boom"}
		require.NoError(t, f.k.SweepExpired(f.ctx))
		ur, _ = f.k.GetUniversalRead(f.ctx, "0xaa")
		require.Equal(t, uint32(i), ur.ExpiryAttempts, "attempt %d", i)
	}

	require.Equal(t, types.UniversalReadStatus_UNIVERSAL_READ_STATUS_ABORTED, ur.Status)
	require.Len(t, ur.PcTx, keeper.MaxExpiryAttempts+1,
		"one fulfil attempt plus a full expiry budget")
}

// Both attempts are visible off-chain: a failed fulfil followed by the sweeper's
// expiry leaves an ordered audit trail on one record.
func TestPcTx_RecordsFulfilThenExpiry(t *testing.T) {
	f := SetupTest(t)
	f.ctx = f.ctx.WithBlockHeight(10)

	seedRead(t, f, "0xaa", 50)
	key := voteToQuorum(t, f, "0xaa", obs(0x01))

	f.evm.vmErrors = []string{"execution reverted"}
	f.evm.revertData = selector("CallerIsNotUCallbackModule()")
	require.NoError(t, fireTerminal(f, key, uvalidatortypes.BallotStatus_BALLOT_STATUS_PASSED))

	f.ctx = f.ctx.WithBlockHeight(100)
	f.evm.revertData = nil
	require.NoError(t, f.k.SweepExpired(f.ctx))

	ur, _ := f.k.GetUniversalRead(f.ctx, "0xaa")
	require.Equal(t, types.UniversalReadStatus_UNIVERSAL_READ_STATUS_EXPIRED, ur.Status)
	require.Len(t, ur.PcTx, 2)
	require.Equal(t, "FAILED", ur.PcTx[0].Status, "the fulfil attempt")
	require.Equal(t, "SUCCESS", ur.PcTx[1].Status, "the expiry that settled it")
}
