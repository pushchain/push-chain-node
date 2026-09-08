package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/x/ucallback/keeper"
	"github.com/pushchain/push-chain-node/x/ucallback/types"
)

func retry(f *testFixture, signer, id string) (*types.MsgRetryReadExpiryResponse, error) {
	return f.msgServer.RetryReadExpiry(f.ctx, &types.MsgRetryReadExpiry{
		Signer: signer, RequestId: id,
	})
}

// The whole point: an abandoned read can be settled after the fact, which is
// otherwise impossible — the sweeper skips ABORTED and the contract admits only
// this module.
func TestRetryReadExpiry_SettlesAbandonedRead(t *testing.T) {
	f := SetupTest(t)
	f.ctx = f.ctx.WithBlockHeight(100)

	abandon(t, f, "0xaa", "RequestNotYetExpired")
	require.Equal(t, []string{"0xaa"}, abortedIDs(t, f))
	callsBefore := len(f.evm.calls)

	// the underlying problem is fixed; the contract now accepts it
	res, err := retry(f, f.uvalidator.admin, "0xaa")
	require.NoError(t, err)
	require.True(t, res.Settled)

	require.Len(t, f.evm.calls, callsBefore+1, "exactly one more attempt")

	ur, _ := f.k.GetUniversalRead(f.ctx, "0xaa")
	require.Equal(t, types.UniversalReadStatus_UNIVERSAL_READ_STATUS_EXPIRED, ur.Status)
	require.Len(t, ur.PcTx, keeper.MaxExpiryAttempts+1, "the retry is on the record too")
	require.Equal(t, "SUCCESS", ur.PcTx[len(ur.PcTx)-1].Status)

	require.Empty(t, abortedIDs(t, f), "off the intervention list")
}

// A failed retry buys one attempt, not a fresh budget — the record stays ABORTED
// with the newest reason.
func TestRetryReadExpiry_FailureStaysAborted(t *testing.T) {
	f := SetupTest(t)
	f.ctx = f.ctx.WithBlockHeight(100)

	abandon(t, f, "0xaa", "first reason")

	f.evm.vmErrors = []string{"still broken"}
	res, err := retry(f, f.uvalidator.admin, "0xaa")
	require.NoError(t, err)
	require.False(t, res.Settled)

	ur, _ := f.k.GetUniversalRead(f.ctx, "0xaa")
	require.Equal(t, types.UniversalReadStatus_UNIVERSAL_READ_STATUS_ABORTED, ur.Status)
	require.Equal(t, "still broken", ur.ErrorMsg, "reason refreshed")
	require.Len(t, ur.PcTx, keeper.MaxExpiryAttempts+1)
	require.Equal(t, []string{"0xaa"}, abortedIDs(t, f), "still needs intervention")

	// and it can be retried again later
	res, err = retry(f, f.uvalidator.admin, "0xaa")
	require.NoError(t, err)
	require.True(t, res.Settled)
}

func TestRetryReadExpiry_RejectsNonAdmin(t *testing.T) {
	f := SetupTest(t)
	f.ctx = f.ctx.WithBlockHeight(100)
	abandon(t, f, "0xaa", "boom")
	callsBefore := len(f.evm.calls)

	_, err := retry(f, "push1nottheadmin", "0xaa")
	require.ErrorContains(t, err, "invalid admin")
	require.Len(t, f.evm.calls, callsBefore, "no contract call from a non-admin")

	ur, _ := f.k.GetUniversalRead(f.ctx, "0xaa")
	require.Equal(t, types.UniversalReadStatus_UNIVERSAL_READ_STATUS_ABORTED, ur.Status)
}

// Only ABORTED is retryable. Anything else either settled cleanly or is still
// moving, and re-running expiry would close a request the chain has no business
// closing.
func TestRetryReadExpiry_RejectsNonAbortedStatuses(t *testing.T) {
	f := SetupTest(t)
	f.ctx = f.ctx.WithBlockHeight(100)

	for _, st := range []types.UniversalReadStatus{
		types.UniversalReadStatus_UNIVERSAL_READ_STATUS_PENDING,
		types.UniversalReadStatus_UNIVERSAL_READ_STATUS_VOTING,
		types.UniversalReadStatus_UNIVERSAL_READ_STATUS_FULFILLED,
		types.UniversalReadStatus_UNIVERSAL_READ_STATUS_EXPIRED,
		types.UniversalReadStatus_UNIVERSAL_READ_STATUS_FAILED,
	} {
		id := "0x" + st.String()
		require.NoError(t, f.k.SetUniversalRead(f.ctx, newRead(id, "0xTX", 500, st)))

		_, err := retry(f, f.uvalidator.admin, id)
		require.ErrorContains(t, err, "only ABORTED", "status %s", st)
	}
	require.Empty(t, f.evm.calls)
}

func TestRetryReadExpiry_Rejects(t *testing.T) {
	f := SetupTest(t)

	_, err := retry(f, f.uvalidator.admin, "0xmissing")
	require.ErrorContains(t, err, "not found")

	_, err = retry(f, f.uvalidator.admin, "")
	require.ErrorContains(t, err, "request_id is required")
}
