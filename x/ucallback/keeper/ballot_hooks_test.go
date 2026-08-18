package keeper_test

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"

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
	key := voteToQuorum(t, f, "0xaa", res)

	require.NoError(t, fireTerminal(f, key, uvalidatortypes.BallotStatus_BALLOT_STATUS_PASSED))

	require.Equal(t, 1, f.evm.callsTo(types.MethodFulfillExternalCallback))
	require.Equal(t, 1, f.evm.callsTo(types.MethodReportCallbackGas),
		"a successful fulfilment settles the gas in the same flow")
	c, ok := f.evm.firstCallTo(types.MethodFulfillExternalCallback)
	require.True(t, ok)
	require.Equal(t, moduleEVMAddr(), c.from, "must be sent as the x/ucallback module account")
	require.True(t, c.isModule)
	require.Nil(t, c.gasLimit, "the contract enforces the callback budget, not us")

	// requestId reaches the contract as a uint256, not a string
	require.Len(t, c.args, 2,
		"fulfillExternalCallback takes only (requestId, resultData)")
	require.Equal(t, big.NewInt(0xaa), c.args[0])
	require.Equal(t, []byte{0x42}, c.args[1])

	ur, _ := f.k.GetUniversalRead(f.ctx, "0xaa")
	require.Equal(t, types.UniversalReadStatus_UNIVERSAL_READ_STATUS_FULFILLED, ur.Status)
	require.Len(t, ur.PcTx, 2, "the fulfil and the gas report are both recorded")
	require.Equal(t, "SUCCESS", ur.PcTx[0].Status)
	require.Equal(t, "0xEVMTX", ur.PcTx[0].TxHash)
	require.Equal(t, moduleEVMAddr().Hex(), ur.PcTx[0].Sender)
	require.Equal(t, "SUCCESS", ur.PcTx[1].Status)

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

	fulfil, ok := f.evm.firstCallTo(types.MethodFulfillExternalCallback)
	require.True(t, ok)
	require.NotNil(t, fulfil.nonce)
	require.Equal(t, uint64(7), *fulfil.nonce, "uses the stored value")

	report, ok := f.evm.firstCallTo(types.MethodReportCallbackGas)
	require.True(t, ok)
	require.Equal(t, uint64(8), *report.nonce, "the report takes the next one")

	got, err := f.k.GetModuleAccountNonce(f.ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(9), got, "both calls advanced it")
}

// Two calls in the same block must not reuse a nonce.
func TestModuleNonce_AdvancesPerCall(t *testing.T) {
	f := SetupTest(t)
	f.ctx = f.ctx.WithBlockHeight(10)

	for i, id := range []string{"0xaa", "0xbb", "0xcc"} {
		seedRead(t, f, id, 500)
		key := voteToQuorum(t, f, id, obs(byte(i+1)))
		require.NoError(t, fireTerminal(f, key, uvalidatortypes.BallotStatus_BALLOT_STATUS_PASSED))
	}

	// every call, fulfil and report alike, draws the next nonce in sequence
	for i, c := range f.evm.calls {
		require.Equal(t, uint64(i), *c.nonce, "call %d", i)
	}

	got, err := f.k.GetModuleAccountNonce(f.ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(len(f.evm.calls)), got)
}

// The address the contract admits is fixed by the module name.
func TestModuleAddress_IsStable(t *testing.T) {
	f := SetupTest(t)
	_, hex := f.k.GetModuleAddress(f.ctx)
	require.Equal(t, "0x07a0258D367A4A4cd9d6E4b7eEE8E7eF491CC519", hex,
		"UniversalCallback hardcodes this; changing the module name breaks every call")
}

// selector returns a Solidity custom-error selector.
func selector(sig string) []byte { return crypto.Keccak256([]byte(sig))[:4] }

// invalidStatus builds an InvalidRequestStatus revert reporting `actual`.
func invalidStatus(actual byte) []byte {
	out := append([]byte{}, selector("InvalidRequestStatus(uint256,uint8,uint8)")...)
	word := func(v byte) []byte { b := make([]byte, 32); b[31] = v; return b }
	out = append(out, word(0xaa)...)
	out = append(out, word(actual)...)
	out = append(out, word(1)...)
	return out
}

// A revert means the whole transaction rolled back: fulfilledRequests stays false,
// _pending survives, _settle never ran, and the funder's deposit is still escrowed.
// Retiring the record would drop it from PendingByExpiry and leave nothing able to
// release those funds — only this module may call expireExternalRead.
func TestAfterBallotTerminal_UnsettledRevertLeavesInFlight(t *testing.T) {
	f := SetupTest(t)
	f.ctx = f.ctx.WithBlockHeight(10)
	f.evm.vmErrors = []string{"execution reverted"}
	f.evm.revertData = selector("CallerIsNotUCallbackModule()")

	seedRead(t, f, "0xaa", 500)
	key := voteToQuorum(t, f, "0xaa", obs(0x01))
	require.NoError(t, fireTerminal(f, key, uvalidatortypes.BallotStatus_BALLOT_STATUS_PASSED))

	ur, _ := f.k.GetUniversalRead(f.ctx, "0xaa")
	require.Equal(t, types.UniversalReadStatus_UNIVERSAL_READ_STATUS_VOTING, ur.Status,
		"nothing settled on the contract, so the read must stay in flight")
	require.NotEmpty(t, ur.ErrorMsg, "but the reason is recorded")
	require.Len(t, ur.PcTx, 1)
	require.Equal(t, "FAILED", ur.PcTx[0].Status)

	// crucially, expiry can still reach it and refund the funder
	require.Equal(t, []string{"0xaa"}, collectDueBy(t, f, 500))
}

// Out of gas is the same: real gas burned, but nothing persisted.
func TestAfterBallotTerminal_OutOfGasLeavesInFlight(t *testing.T) {
	f := SetupTest(t)
	f.ctx = f.ctx.WithBlockHeight(10)
	f.evm.vmErrors = []string{"out of gas"}

	seedRead(t, f, "0xaa", 500)
	key := voteToQuorum(t, f, "0xaa", obs(0x01))
	require.NoError(t, fireTerminal(f, key, uvalidatortypes.BallotStatus_BALLOT_STATUS_PASSED))

	ur, _ := f.k.GetUniversalRead(f.ctx, "0xaa")
	require.Equal(t, types.UniversalReadStatus_UNIVERSAL_READ_STATUS_VOTING, ur.Status)
	require.Equal(t, []string{"0xaa"}, collectDueBy(t, f, 500))
}

// RequestAlreadyFulfilled is the one revert that IS terminal — the contract closed
// the request another way and the funder already has their refund.
func TestAfterBallotTerminal_AlreadySettledIsTerminal(t *testing.T) {
	f := SetupTest(t)
	f.ctx = f.ctx.WithBlockHeight(10)
	f.evm.vmErrors = []string{"execution reverted"}
	f.evm.revertData = invalidStatus(3) // SETTLED

	seedRead(t, f, "0xaa", 500)
	key := voteToQuorum(t, f, "0xaa", obs(0x01))
	require.NoError(t, fireTerminal(f, key, uvalidatortypes.BallotStatus_BALLOT_STATUS_PASSED))

	ur, _ := f.k.GetUniversalRead(f.ctx, "0xaa")
	require.Equal(t, types.UniversalReadStatus_UNIVERSAL_READ_STATUS_FAILED, ur.Status)
	require.Empty(t, collectDueBy(t, f, 500), "settled, so nothing left to expire")
}

// A dispatch error produces no response at all — still unsettled.
func TestAfterBallotTerminal_CallErrorLeavesInFlight(t *testing.T) {
	f := SetupTest(t)
	f.ctx = f.ctx.WithBlockHeight(10)

	seedRead(t, f, "0xaa", 500)
	key := voteToQuorum(t, f, "0xaa", obs(0x01))
	f.evm.callErr = errTest

	require.NoError(t, fireTerminal(f, key, uvalidatortypes.BallotStatus_BALLOT_STATUS_PASSED))

	ur, _ := f.k.GetUniversalRead(f.ctx, "0xaa")
	require.Equal(t, types.UniversalReadStatus_UNIVERSAL_READ_STATUS_VOTING, ur.Status)
	require.Contains(t, ur.ErrorMsg, "injected")
	require.Equal(t, []string{"0xaa"}, collectDueBy(t, f, 500))
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

	require.Equal(t, 1, f.evm.callsTo(types.MethodFulfillExternalCallback),
		"the contract must be fulfilled exactly once")
	require.Equal(t, 1, f.evm.callsTo(types.MethodReportCallbackGas),
		"and settled exactly once")
}

// Neither EXPIRED nor REJECTED retires a request here — expiry belongs to the
// sweeper, which sees every overdue request rather than only those with a ballot.
func TestAfterBallotTerminal_NonPassedLeavesToSweeper(t *testing.T) {
	for _, status := range []uvalidatortypes.BallotStatus{
		uvalidatortypes.BallotStatus_BALLOT_STATUS_EXPIRED,
		uvalidatortypes.BallotStatus_BALLOT_STATUS_REJECTED,
	} {
		t.Run(status.String(), func(t *testing.T) {
			f := SetupTest(t)
			f.ctx = f.ctx.WithBlockHeight(10)

			seedRead(t, f, "0xaa", 500)
			key := voteToQuorum(t, f, "0xaa", obs(0x01))

			require.NoError(t, fireTerminal(f, key, status))

			require.Empty(t, f.evm.calls, "the hook must not call the contract")
			ur, _ := f.k.GetUniversalRead(f.ctx, "0xaa")
			require.Equal(t, types.UniversalReadStatus_UNIVERSAL_READ_STATUS_VOTING, ur.Status)
			require.Equal(t, []string{"0xaa"}, pendingIDs(t, f),
				"still in flight, so the sweeper can find it")
		})
	}
}

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
