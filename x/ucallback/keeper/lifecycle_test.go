package keeper_test

import (
	"testing"

	sdkmath "cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/x/ucallback/keeper"
	"github.com/pushchain/push-chain-node/x/ucallback/types"
	uvalidatortypes "github.com/pushchain/push-chain-node/x/uvalidator/types"
)

// End-to-end through the real path: an EVM log becomes a record, validators poll
// and vote it to quorum, the ballot hook fulfils, and the consumed gas is reported
// and burned. Each stage is driven by the same entry point production uses.
func TestLifecycle_IngestToBurn(t *testing.T) {
	f := SetupTest(t)
	f.ctx = f.ctx.WithBlockHeight(100)
	v := seedVoters(t, f, 4)

	// ── ingest ──
	lg := readLog(t, "0xaa", 5_000, 0)
	require.NoError(t, f.k.IngestReadRequests(f.ctx, receipt("0xTX", lg)))

	id := lg.Topics[1]
	ur, found := f.k.GetUniversalRead(f.ctx, id)
	require.True(t, found)
	require.Equal(t, types.UniversalReadStatus_UNIVERSAL_READ_STATUS_PENDING, ur.Status)
	require.Equal(t, uint64(250_000), ur.Request.CallbackGasLimit)
	require.Equal(t, "39", ur.Request.CallbackBudget)

	// ── validators poll ──
	require.Equal(t, []string{id}, pendingIDs(t, f), "offered for observation")

	// the log's budget of 39 wei cannot cover 250k gas at 1 gwei, so fund it
	ur.Request.CallbackBudget = "1000000000000000000"
	require.NoError(t, f.k.SetUniversalRead(f.ctx, ur))

	// ── vote to quorum ──
	for i := 0; i < 2; i++ {
		finalized, err := f.k.VoteReadResult(f.ctx, v[i], id, obs(0x42))
		require.NoError(t, err)
		require.False(t, finalized)
	}
	finalized, err := f.k.VoteReadResult(f.ctx, v[2], id, obs(0x42))
	require.NoError(t, err)
	require.True(t, finalized, "3 of 4 carries it")

	// ── the ballot hook fulfils and settles ──
	ur, _ = f.k.GetUniversalRead(f.ctx, id)
	require.NoError(t, fireTerminal(f, ur.BallotKey,
		uvalidatortypes.BallotStatus_BALLOT_STATUS_PASSED))

	require.Equal(t, 1, f.evm.callsTo(types.MethodFulfillExternalCallback))
	require.Equal(t, 1, f.evm.callsTo(types.MethodReportCallbackGas))

	want := sdkmath.NewInt(21_000).Mul(sdkmath.NewInt(1_000_000_000))
	require.Equal(t, want, f.bank.burned, "gasUsed × baseFee destroyed")

	// ── final state ──
	done, _ := f.k.GetUniversalRead(f.ctx, id)
	require.Equal(t, types.UniversalReadStatus_UNIVERSAL_READ_STATUS_FULFILLED, done.Status)
	require.Equal(t, []byte{0x42}, done.Result.ResultData)
	require.Empty(t, done.ErrorMsg)
	require.Len(t, done.PcTx, 2, "the fulfil and the report")

	require.Empty(t, pendingIDs(t, f), "no longer offered")
	require.Empty(t, collectDueBy(t, f, 9_999), "and out of the sweeper's reach")

	// the sweeper must not touch a fulfilled read even long past its deadline
	f.ctx = f.ctx.WithBlockHeight(50_000)
	before := len(f.evm.calls)
	require.NoError(t, f.k.SweepExpired(f.ctx))
	require.Equal(t, before, len(f.evm.calls))
}

// The other terminal path end-to-end: nobody votes, the deadline passes, the
// sweeper expires it and nothing is burned.
func TestLifecycle_IngestToExpiry(t *testing.T) {
	f := SetupTest(t)
	f.ctx = f.ctx.WithBlockHeight(100)

	lg := readLog(t, "0xaa", 200, 0)
	require.NoError(t, f.k.IngestReadRequests(f.ctx, receipt("0xTX", lg)))
	id := lg.Topics[1]

	f.ctx = f.ctx.WithBlockHeight(300) // past the deadline
	require.NoError(t, f.k.SweepExpired(f.ctx))

	ur, _ := f.k.GetUniversalRead(f.ctx, id)
	require.Equal(t, types.UniversalReadStatus_UNIVERSAL_READ_STATUS_EXPIRED, ur.Status)
	require.Equal(t, uint32(1), ur.ExpiryAttempts)
	require.Equal(t, 1, f.evm.callsTo(types.MethodExpireExternalRead))
	require.Equal(t, 0, f.evm.callsTo(types.MethodReportCallbackGas), "nothing to report")
	require.True(t, f.bank.burned.IsZero(), "an unexecuted callback burns nothing")
}

// An underfunded read runs the whole way through without ever touching the
// contract, and its reason survives onto the expired record.
func TestLifecycle_UnderfundedExpiresWithReason(t *testing.T) {
	f := SetupTest(t)
	f.ctx = f.ctx.WithBlockHeight(100)
	v := seedVoters(t, f, 4)

	// budget of 39 wei against a 250k gas limit — nowhere near enough
	lg := readLog(t, "0xaa", 500, 0)
	require.NoError(t, f.k.IngestReadRequests(f.ctx, receipt("0xTX", lg)))
	id := lg.Topics[1]

	for i := 0; i < 3; i++ {
		_, err := f.k.VoteReadResult(f.ctx, v[i], id, obs(0x01))
		require.NoError(t, err)
	}
	ur, _ := f.k.GetUniversalRead(f.ctx, id)
	require.NoError(t, fireTerminal(f, ur.BallotKey,
		uvalidatortypes.BallotStatus_BALLOT_STATUS_PASSED))

	require.Equal(t, 0, f.evm.callsTo(types.MethodFulfillExternalCallback),
		"never executed")
	ur, _ = f.k.GetUniversalRead(f.ctx, id)
	require.Equal(t, keeper.ErrBudgetTooSmall, ur.ErrorMsg)

	// and the sweeper still retires it, refunding the whole budget
	f.ctx = f.ctx.WithBlockHeight(600)
	require.NoError(t, f.k.SweepExpired(f.ctx))

	ur, _ = f.k.GetUniversalRead(f.ctx, id)
	require.Equal(t, types.UniversalReadStatus_UNIVERSAL_READ_STATUS_EXPIRED, ur.Status)
	require.Equal(t, keeper.ErrBudgetTooSmall, ur.ErrorMsg,
		"the reason it was never fulfilled survives onto the expired record")
	require.True(t, f.bank.burned.IsZero())
}
