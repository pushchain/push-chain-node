package keeper_test

import (
	"math/big"
	"testing"

	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/ethereum/go-ethereum/common"

	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/x/ucallback/keeper"
	"github.com/pushchain/push-chain-node/x/ucallback/types"
	uvalidatortypes "github.com/pushchain/push-chain-node/x/uvalidator/types"
)

// moduleEVMAddr is the address UniversalCallback admits as caller.
func moduleEVMAddr() common.Address {
	var a common.Address
	copy(a[:], authtypes.NewModuleAddress(types.ModuleName).Bytes())
	return a
}

// voteToQuorum drives a request to a finalized ballot and returns its key.
func voteToQuorum(t *testing.T, f *testFixture, id string, r *types.ReadResult) string {
	t.Helper()
	v := seedVoters(t, f, 4)
	for i := 0; i < 3; i++ {
		if _, err := f.k.VoteReadResult(f.ctx, v[i], id, r); err != nil {
			t.Fatalf("vote %d: %v", i, err)
		}
	}
	ur, found := f.k.GetUniversalRead(f.ctx, id)
	require.True(t, found)
	return ur.BallotKey
}

func fireTerminal(f *testFixture, ballotKey string, status uvalidatortypes.BallotStatus) error {
	return keeper.NewBallotHooks(f.k).AfterBallotTerminal(
		f.ctx, ballotKey,
		uvalidatortypes.BallotObservationType_BALLOT_OBSERVATION_TYPE_READ_RESULT,
		status,
	)
}

// The happy path: a passed ballot calls the contract and marks the read fulfilled.
func TestAfterBallotTerminal_FulfilsOnPassed(t *testing.T) {
	f := SetupTest(t)
	f.ctx = f.ctx.WithBlockHeight(10)
	seedRead(t, f, "0xaa", 500)

	res := obs(0x42)
	res.ObservedBlockHeight = 8_000_123
	res.ObservedBlockHash = blockHash(0xef)
	key := voteToQuorum(t, f, "0xaa", res)

	require.NoError(t, fireTerminal(f, key, uvalidatortypes.BallotStatus_BALLOT_STATUS_PASSED))

	require.Len(t, f.evm.calls, 1, "exactly one contract call")
	c := f.evm.lastCall()
	require.Equal(t, types.MethodFulfillExternalCallback, c.method)
	require.Equal(t, moduleEVMAddr(), c.from, "must be sent as the x/ucallback module account")
	require.True(t, c.isModule)
	require.Nil(t, c.gasLimit, "the contract enforces the callback budget, not us")

	// requestId reaches the contract as a uint256, not a string
	require.Equal(t, big.NewInt(0xaa), c.args[0])
	require.Equal(t, []byte{0x42}, c.args[1])
	require.Equal(t, uint64(8_000_123), c.args[2])
	require.Equal(t, [32]byte{31: 0xef}, c.args[3], "32-byte hash passes through unchanged")

	ur, _ := f.k.GetUniversalRead(f.ctx, "0xaa")
	require.Equal(t, types.UniversalReadStatus_UNIVERSAL_READ_STATUS_FULFILLED, ur.Status)
	require.Len(t, ur.PcTx, 1)
	require.Equal(t, "SUCCESS", ur.PcTx[0].Status)
	require.Equal(t, "0xEVMTX", ur.PcTx[0].TxHash)
	require.Equal(t, moduleEVMAddr().Hex(), ur.PcTx[0].Sender)

	// settled, so it leaves the in-flight set
	require.Empty(t, pendingIDs(t, f))
}

// The module account nonce lives in x/ucallback's own state, and every call must
// draw and advance it — a reused nonce is rejected by the EVM.
func TestFulfil_UsesAndAdvancesModuleNonce(t *testing.T) {
	f := SetupTest(t)
	f.ctx = f.ctx.WithBlockHeight(10)
	require.NoError(t, f.k.ModuleAccountNonce.Set(f.ctx, 7))

	seedRead(t, f, "0xaa", 500)
	key := voteToQuorum(t, f, "0xaa", obs(0x01))
	require.NoError(t, fireTerminal(f, key, uvalidatortypes.BallotStatus_BALLOT_STATUS_PASSED))

	require.NotNil(t, f.evm.lastCall().nonce)
	require.Equal(t, uint64(7), *f.evm.lastCall().nonce, "uses the stored value")

	got, err := f.k.GetModuleAccountNonce(f.ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(8), got, "and advances it")
}

// Two calls in the same block must not reuse a nonce.
func TestModuleNonce_AdvancesPerCall(t *testing.T) {
	f := SetupTest(t)
	f.ctx = f.ctx.WithBlockHeight(10)

	for i, id := range []string{"0xaa", "0xbb", "0xcc"} {
		seedRead(t, f, id, 500)
		key := voteToQuorum(t, f, id, obs(byte(i+1)))
		require.NoError(t, fireTerminal(f, key, uvalidatortypes.BallotStatus_BALLOT_STATUS_PASSED))
		require.Equal(t, uint64(i), *f.evm.calls[i].nonce, "call %d", i)
	}

	got, err := f.k.GetModuleAccountNonce(f.ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(3), got)
}

// The address the contract admits is fixed by the module name.
func TestModuleAddress_IsStable(t *testing.T) {
	f := SetupTest(t)
	_, hex := f.k.GetModuleAddress(f.ctx)
	require.Equal(t, "0x07a0258D367A4A4cd9d6E4b7eEE8E7eF491CC519", hex,
		"UniversalCallback hardcodes this; changing the module name breaks every call")
}

// A reverting callback still closes the request: the contract sets
// fulfilledRequests before invoking it, so retrying could never succeed.
func TestAfterBallotTerminal_CallbackRevertMarksFailed(t *testing.T) {
	f := SetupTest(t)
	f.ctx = f.ctx.WithBlockHeight(10)
	f.evm.vmErrors = []string{"execution reverted"}

	seedRead(t, f, "0xaa", 500)
	key := voteToQuorum(t, f, "0xaa", obs(0x01))
	require.NoError(t, fireTerminal(f, key, uvalidatortypes.BallotStatus_BALLOT_STATUS_PASSED))

	ur, _ := f.k.GetUniversalRead(f.ctx, "0xaa")
	require.Equal(t, types.UniversalReadStatus_UNIVERSAL_READ_STATUS_FAILED, ur.Status,
		"a reverted callback is terminal, not retryable")
	require.Len(t, ur.PcTx, 1)
	require.Equal(t, "FAILED", ur.PcTx[0].Status)
	require.Equal(t, "execution reverted", ur.PcTx[0].ErrorMsg)

	require.Empty(t, pendingIDs(t, f), "must not be offered to validators again")
}

// An infrastructure error is recorded the same way — the attempt is still logged.
func TestAfterBallotTerminal_CallErrorIsRecorded(t *testing.T) {
	f := SetupTest(t)
	f.ctx = f.ctx.WithBlockHeight(10)

	seedRead(t, f, "0xaa", 500)
	key := voteToQuorum(t, f, "0xaa", obs(0x01))
	f.evm.callErr = errTest

	require.NoError(t, fireTerminal(f, key, uvalidatortypes.BallotStatus_BALLOT_STATUS_PASSED))

	ur, _ := f.k.GetUniversalRead(f.ctx, "0xaa")
	require.Equal(t, types.UniversalReadStatus_UNIVERSAL_READ_STATUS_FAILED, ur.Status)
	require.Contains(t, ur.PcTx[0].ErrorMsg, "injected")
}

// Re-firing the hook must not call the contract twice. This is what makes the
// "settled reads are not findable by ballot" semantic from C3 load-bearing.
func TestAfterBallotTerminal_IsIdempotent(t *testing.T) {
	f := SetupTest(t)
	f.ctx = f.ctx.WithBlockHeight(10)

	seedRead(t, f, "0xaa", 500)
	key := voteToQuorum(t, f, "0xaa", obs(0x01))

	require.NoError(t, fireTerminal(f, key, uvalidatortypes.BallotStatus_BALLOT_STATUS_PASSED))
	require.NoError(t, fireTerminal(f, key, uvalidatortypes.BallotStatus_BALLOT_STATUS_PASSED))
	require.NoError(t, fireTerminal(f, key, uvalidatortypes.BallotStatus_BALLOT_STATUS_PASSED))

	require.Len(t, f.evm.calls, 1, "the contract must be called exactly once")

	ur, _ := f.k.GetUniversalRead(f.ctx, "0xaa")
	require.Len(t, ur.PcTx, 1, "and only one attempt recorded")
}

// A rejected ballot is not a deadline: the request keeps its remaining time and
// other observations may still win.
func TestAfterBallotTerminal_RejectedLeavesInFlight(t *testing.T) {
	f := SetupTest(t)
	f.ctx = f.ctx.WithBlockHeight(10)

	seedRead(t, f, "0xaa", 500)
	key := voteToQuorum(t, f, "0xaa", obs(0x01))

	require.NoError(t, fireTerminal(f, key, uvalidatortypes.BallotStatus_BALLOT_STATUS_REJECTED))

	require.Empty(t, f.evm.calls, "no contract call for a rejected ballot")
	ur, _ := f.k.GetUniversalRead(f.ctx, "0xaa")
	require.Equal(t, types.UniversalReadStatus_UNIVERSAL_READ_STATUS_VOTING, ur.Status)
	require.Equal(t, []string{"0xaa"}, pendingIDs(t, f),
		"still offered — validators may yet agree")
}

// The ballot carries the request's deadline, so an expired ballot means the request
// is over. Retire it against the contract rather than waiting for the sweeper.
func TestAfterBallotTerminal_ExpiredRetiresRequest(t *testing.T) {
	f := SetupTest(t)
	f.ctx = f.ctx.WithBlockHeight(10)

	seedRead(t, f, "0xaa", 500)
	key := voteToQuorum(t, f, "0xaa", obs(0x01))

	require.NoError(t, fireTerminal(f, key, uvalidatortypes.BallotStatus_BALLOT_STATUS_EXPIRED))

	require.Len(t, f.evm.calls, 1)
	require.Equal(t, types.MethodExpireExternalRead, f.evm.lastCall().method)
	require.Equal(t, big.NewInt(0xaa), f.evm.lastCall().args[0])

	ur, _ := f.k.GetUniversalRead(f.ctx, "0xaa")
	require.Equal(t, types.UniversalReadStatus_UNIVERSAL_READ_STATUS_EXPIRED, ur.Status)
	require.Len(t, ur.PcTx, 1)
	require.Equal(t, "SUCCESS", ur.PcTx[0].Status)
	require.Empty(t, pendingIDs(t, f))
}

// A revert on expiry still retires the request — almost always
// RequestAlreadyFulfilled, and in every case the chain is done with it.
func TestAfterBallotTerminal_ExpiryRevertStillRetires(t *testing.T) {
	f := SetupTest(t)
	f.ctx = f.ctx.WithBlockHeight(10)
	f.evm.vmErrors = []string{"RequestAlreadyFulfilled"}

	seedRead(t, f, "0xaa", 500)
	key := voteToQuorum(t, f, "0xaa", obs(0x01))

	require.NoError(t, fireTerminal(f, key, uvalidatortypes.BallotStatus_BALLOT_STATUS_EXPIRED))

	ur, _ := f.k.GetUniversalRead(f.ctx, "0xaa")
	require.Equal(t, types.UniversalReadStatus_UNIVERSAL_READ_STATUS_EXPIRED, ur.Status)
	require.Equal(t, "FAILED", ur.PcTx[0].Status)
	require.Empty(t, pendingIDs(t, f), "not retried")
}

// Ballots belonging to other modules must be ignored outright.
func TestAfterBallotTerminal_IgnoresOtherBallotTypes(t *testing.T) {
	f := SetupTest(t)
	f.ctx = f.ctx.WithBlockHeight(10)

	seedRead(t, f, "0xaa", 500)
	key := voteToQuorum(t, f, "0xaa", obs(0x01))

	for _, bt := range []uvalidatortypes.BallotObservationType{
		uvalidatortypes.BallotObservationType_BALLOT_OBSERVATION_TYPE_INBOUND_TX,
		uvalidatortypes.BallotObservationType_BALLOT_OBSERVATION_TYPE_OUTBOUND_TX,
		uvalidatortypes.BallotObservationType_BALLOT_OBSERVATION_TYPE_TSS_KEY,
	} {
		require.NoError(t, keeper.NewBallotHooks(f.k).AfterBallotTerminal(
			f.ctx, key, bt, uvalidatortypes.BallotStatus_BALLOT_STATUS_PASSED))
	}

	require.Empty(t, f.evm.calls, "only READ_RESULT ballots may drive fulfilment")
}

// An unknown ballot is not an error — the hook fires for every module's ballots.
func TestAfterBallotTerminal_UnknownBallotIsNoop(t *testing.T) {
	f := SetupTest(t)
	require.NoError(t, fireTerminal(f, "not-a-ballot-of-ours",
		uvalidatortypes.BallotStatus_BALLOT_STATUS_PASSED))
	require.Empty(t, f.evm.calls)
}

// Batched siblings settle independently: fulfilling one must not touch the other.
func TestAfterBallotTerminal_BatchSiblingsIndependent(t *testing.T) {
	f := SetupTest(t)
	f.ctx = f.ctx.WithBlockHeight(10)

	require.NoError(t, f.k.SetUniversalRead(f.ctx,
		newRead("0xaa", "0xBATCH", 500, types.UniversalReadStatus_UNIVERSAL_READ_STATUS_PENDING)))
	require.NoError(t, f.k.SetUniversalRead(f.ctx,
		newRead("0xbb", "0xBATCH", 500, types.UniversalReadStatus_UNIVERSAL_READ_STATUS_PENDING)))

	key := voteToQuorum(t, f, "0xaa", obs(0x01))
	require.NoError(t, fireTerminal(f, key, uvalidatortypes.BallotStatus_BALLOT_STATUS_PASSED))

	a, _ := f.k.GetUniversalRead(f.ctx, "0xaa")
	b, _ := f.k.GetUniversalRead(f.ctx, "0xbb")
	require.Equal(t, types.UniversalReadStatus_UNIVERSAL_READ_STATUS_FULFILLED, a.Status)
	require.Equal(t, types.UniversalReadStatus_UNIVERSAL_READ_STATUS_PENDING, b.Status)
	require.Equal(t, []string{"0xbb"}, pendingIDs(t, f))
}
