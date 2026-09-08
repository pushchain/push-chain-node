package integrationtest

import (
	"math/big"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	utils "github.com/pushchain/push-chain-node/test/utils"
	pchaintypes "github.com/pushchain/push-chain-node/types"
	ucallbackkeeper "github.com/pushchain/push-chain-node/x/ucallback/keeper"
	ucallbacktypes "github.com/pushchain/push-chain-node/x/ucallback/types"
	uvalidatortypes "github.com/pushchain/push-chain-node/x/uvalidator/types"
)

// The query surface is what validators poll to decide what to observe and what
// operators read to see why something stalled. Wrong answers here are not cosmetic:
// AllPendingReadRequests is the work queue.
func TestQueries_ServeRealState(t *testing.T) {
	chainApp, ctx, _ := utils.SetAppWithValidators(t)
	k := chainApp.UcallbackKeeper
	q := ucallbackkeeper.NewQuerier(k)

	require.NoError(t, k.InitGenesis(ctx, ucallbacktypes.DefaultGenesis()))

	live := newReadFixture(t, chainApp, ctx, 0x41, big.NewInt(3_000_000_000_000_000), 200_000,
		uint64(ctx.BlockHeight())+1000)

	t.Run("Params", func(t *testing.T) {
		res, err := q.Params(ctx, &ucallbacktypes.QueryParamsRequest{})
		require.NoError(t, err)
		require.NotNil(t, res.Params)
	})

	t.Run("UniversalRead returns the record", func(t *testing.T) {
		res, err := q.UniversalRead(ctx, &ucallbacktypes.QueryUniversalReadRequest{
			RequestId: live.idHex,
		})
		require.NoError(t, err)
		require.Equal(t, live.idHex, res.Read.Id)
	})

	t.Run("UniversalRead errors on an unknown id", func(t *testing.T) {
		_, err := q.UniversalRead(ctx, &ucallbacktypes.QueryUniversalReadRequest{
			RequestId: "0xdoesnotexist",
		})
		require.Error(t, err)
	})

	t.Run("AllPendingReadRequests lists the in-flight read", func(t *testing.T) {
		res, err := q.AllPendingReadRequests(ctx,
			&ucallbacktypes.QueryAllPendingReadRequestsRequest{})
		require.NoError(t, err)
		require.True(t, containsRead(res.Reads, live.idHex),
			"an unsettled read must appear in the validator work queue")
	})

	t.Run("ReadsByTx reassembles by originating tx", func(t *testing.T) {
		res, err := q.ReadsByTx(ctx, &ucallbacktypes.QueryReadsByTxRequest{TxHash: "0xabc"})
		require.NoError(t, err)
		require.True(t, containsRead(res.Reads, live.idHex))
	})

	t.Run("AllAbortedReadRequests is empty until something aborts", func(t *testing.T) {
		res, err := q.AllAbortedReadRequests(ctx,
			&ucallbacktypes.QueryAllAbortedReadRequestsRequest{})
		require.NoError(t, err)
		require.False(t, containsRead(res.Reads, live.idHex))
	})
}

// A settled read must leave the pending queue, or validators keep observing work
// that is already done.
func TestQueries_SettledReadLeavesThePendingQueue(t *testing.T) {
	chainApp, ctx, _ := utils.SetAppWithValidators(t)
	k := chainApp.UcallbackKeeper
	q := ucallbackkeeper.NewQuerier(k)
	require.NoError(t, k.InitGenesis(ctx, ucallbacktypes.DefaultGenesis()))

	f := newReadFixture(t, chainApp, ctx, 0x42, big.NewInt(3_000_000_000_000_000), 200_000,
		uint64(ctx.BlockHeight())+1000)

	require.NoError(t, k.FulfilRead(ctx, f.universalRead(
		ucallbacktypes.UniversalReadStatus_UNIVERSAL_READ_STATUS_VOTING, okResult())))

	res, err := q.AllPendingReadRequests(ctx, &ucallbacktypes.QueryAllPendingReadRequestsRequest{})
	require.NoError(t, err)
	require.False(t, containsRead(res.Reads, f.idHex),
		"a fulfilled read must not still be offered as work")
}

// The admin escape hatch is the only way an ABORTED read ever releases its escrow:
// the sweeper has given up on it and the contract admits no other caller.
func TestRetryReadExpiry_AdminOnlyAndRecovers(t *testing.T) {
	chainApp, ctx, _ := utils.SetAppWithValidators(t)
	k := chainApp.UcallbackKeeper
	ms := ucallbackkeeper.NewMsgServerImpl(k)

	// Seed the admin BEFORE anything asserts on authorization: without it GetAdmin
	// errors, and the "non-admin is refused" case below would pass on the lookup
	// failure rather than on the check it claims to exercise.
	admin := sdk.AccAddress([]byte("ucallback-admin-acct")).String()
	require.NoError(t, chainApp.UvalidatorKeeper.Params.Set(ctx,
		uvalidatortypes.Params{Admin: admin}))

	expiry := uint64(ctx.BlockHeight()) + 2
	budget := big.NewInt(2_000_000_000_000_000)
	f := newReadFixture(t, chainApp, ctx, 0x43, budget, 200_000, expiry)

	// park it in ABORTED, the state nothing else can leave
	aborted := f.universalRead(ucallbacktypes.UniversalReadStatus_UNIVERSAL_READ_STATUS_ABORTED, nil)
	aborted.ErrorMsg = "gave up"
	require.NoError(t, k.SetUniversalRead(ctx, aborted))

	q := ucallbackkeeper.NewQuerier(k)
	listed, err := q.AllAbortedReadRequests(ctx, &ucallbacktypes.QueryAllAbortedReadRequestsRequest{})
	require.NoError(t, err)
	require.True(t, containsRead(listed.Reads, f.idHex),
		"an abandoned read must be discoverable, or nobody knows to retry it")

	t.Run("a non-admin is refused", func(t *testing.T) {
		_, err := ms.RetryReadExpiry(ctx, &ucallbacktypes.MsgRetryReadExpiry{
			Signer:    sdk.AccAddress([]byte("some-random-signer!!")).String(),
			RequestId: f.idHex,
		})
		require.Error(t, err, "only the uvalidator admin may drive the escape hatch")
	})

	t.Run("the admin recovers the escrow", func(t *testing.T) {
		past := ctx.WithBlockHeight(int64(expiry) + 1)
		recipientBefore := chainApp.BankKeeper.GetBalance(past,
			sdk.AccAddress(f.recipient.Bytes()), pchaintypes.BaseDenom).Amount

		_, err := ms.RetryReadExpiry(past, &ucallbacktypes.MsgRetryReadExpiry{
			Signer: admin, RequestId: f.idHex,
		})
		require.NoError(t, err)

		got, _ := k.GetUniversalRead(past, f.idHex)
		require.Equal(t, ucallbacktypes.UniversalReadStatus_UNIVERSAL_READ_STATUS_EXPIRED, got.Status,
			"a successful retry must retire the read; err=%q", got.ErrorMsg)

		recipientAfter := chainApp.BankKeeper.GetBalance(past,
			sdk.AccAddress(f.recipient.Bytes()), pchaintypes.BaseDenom).Amount
		require.True(t, recipientAfter.GT(recipientBefore),
			"the whole point of the hatch is that the funder gets paid")

		// and it must drop off the aborted list
		after, err := q.AllAbortedReadRequests(past,
			&ucallbacktypes.QueryAllAbortedReadRequestsRequest{})
		require.NoError(t, err)
		require.False(t, containsRead(after.Reads, f.idHex))
	})
}

func containsRead(reads []ucallbacktypes.UniversalRead, id string) bool {
	for _, r := range reads {
		if r.Id == id {
			return true
		}
	}
	return false
}
