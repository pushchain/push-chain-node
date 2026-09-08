package keeper_test

import (
	"math/big"
	"testing"

	sdkmath "cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/x/ucallback/keeper"
	"github.com/pushchain/push-chain-node/x/ucallback/types"
	uvalidatortypes "github.com/pushchain/push-chain-node/x/uvalidator/types"
)

// fundedRead seeds a read with an explicit gas limit and budget.
func fundedRead(t *testing.T, f *testFixture, id string, expiry uint64, gasLimit uint64, budget string) {
	t.Helper()
	ur := newRead(id, "0xTX", expiry, types.UniversalReadStatus_UNIVERSAL_READ_STATUS_PENDING)
	ur.Request.CallbackGasLimit = gasLimit
	ur.Request.CallbackBudget = budget
	require.NoError(t, f.k.SetUniversalRead(f.ctx, ur))
}

// ── affordability ────────────────────────────────────────────────────────────

// The gate is all-or-nothing: a budget short of the declared gas means no call at
// all, so the funder is refunded in full rather than charged for a doomed attempt.
func TestFulfil_UnderfundedIsNotExecuted(t *testing.T) {
	f := SetupTest(t)
	f.ctx = f.ctx.WithBlockHeight(10)

	// 250k gas at 1 gwei costs 2.5e14; fund one wei short of it
	fundedRead(t, f, "0xaa", 500, 250_000, "249999999999999")
	key := voteToQuorum(t, f, "0xaa", obs(0x01))

	require.NoError(t, fireTerminal(f, key, uvalidatortypes.BallotStatus_BALLOT_STATUS_PASSED))

	require.Empty(t, f.evm.calls, "the contract must not be touched")
	require.True(t, f.bank.burned.IsZero(), "nothing burned")

	ur, _ := f.k.GetUniversalRead(f.ctx, "0xaa")
	require.Equal(t, keeper.ErrBudgetTooSmall, ur.ErrorMsg, "the reason is on the record")
	require.Equal(t, types.UniversalReadStatus_UNIVERSAL_READ_STATUS_VOTING, ur.Status,
		"left in flight so the sweeper refunds the full budget")
	require.Equal(t, []string{"0xaa"}, collectDueBy(t, f, 500))
}

// Exactly enough is enough — the boundary must not be off by one.
func TestFulfil_ExactlyAffordableExecutes(t *testing.T) {
	f := SetupTest(t)
	f.ctx = f.ctx.WithBlockHeight(10)

	fundedRead(t, f, "0xaa", 500, 250_000, "250000000000000") // 250k × 1 gwei
	key := voteToQuorum(t, f, "0xaa", obs(0x01))

	require.NoError(t, fireTerminal(f, key, uvalidatortypes.BallotStatus_BALLOT_STATUS_PASSED))

	require.Equal(t, 1, f.evm.callsTo(types.MethodFulfillExternalCallback))
	ur, _ := f.k.GetUniversalRead(f.ctx, "0xaa")
	require.Equal(t, types.UniversalReadStatus_UNIVERSAL_READ_STATUS_FULFILLED, ur.Status)
}

// A zero budget is legal on the contract but funds nothing, so it never executes.
func TestFulfil_ZeroBudgetIsNotExecuted(t *testing.T) {
	f := SetupTest(t)
	f.ctx = f.ctx.WithBlockHeight(10)

	fundedRead(t, f, "0xaa", 500, 250_000, "0")
	key := voteToQuorum(t, f, "0xaa", obs(0x01))

	require.NoError(t, fireTerminal(f, key, uvalidatortypes.BallotStatus_BALLOT_STATUS_PASSED))
	require.Empty(t, f.evm.calls)
	ur, _ := f.k.GetUniversalRead(f.ctx, "0xaa")
	require.Equal(t, keeper.ErrBudgetTooSmall, ur.ErrorMsg)
}

// ── settlement ───────────────────────────────────────────────────────────────

// The happy path: report what the callback cost, then destroy exactly that.
func TestSettle_ReportsThenBurns(t *testing.T) {
	f := SetupTest(t)
	f.ctx = f.ctx.WithBlockHeight(10)

	fundedRead(t, f, "0xaa", 500, 250_000, "1000000000000000000")
	key := voteToQuorum(t, f, "0xaa", obs(0x01))
	require.NoError(t, fireTerminal(f, key, uvalidatortypes.BallotStatus_BALLOT_STATUS_PASSED))

	// the fake receipt reports 21000 gas; at 1 gwei that is 2.1e13
	want := sdkmath.NewInt(21_000).Mul(sdkmath.NewInt(1_000_000_000))

	report, ok := f.evm.firstCallTo(types.MethodReportCallbackGas)
	require.True(t, ok, "the gas must be reported")
	require.Equal(t, want.BigInt(), report.args[1], "reported cost = gasUsed × baseFee")

	require.Equal(t, want, f.bank.burned, "burn exactly what was reported")
	require.Equal(t, want, f.bank.sentAmount, "and take exactly that from the contract")
	require.Equal(t, types.ModuleName, f.bank.sentTo)
	require.Equal(t, types.ModuleName, f.bank.burnedFrom)
}

// The coins come out of UniversalCallback itself, not from anywhere else.
func TestSettle_TakesFromTheContract(t *testing.T) {
	f := SetupTest(t)
	f.ctx = f.ctx.WithBlockHeight(10)

	fundedRead(t, f, "0xaa", 500, 250_000, "1000000000000000000")
	key := voteToQuorum(t, f, "0xaa", obs(0x01))
	require.NoError(t, fireTerminal(f, key, uvalidatortypes.BallotStatus_BALLOT_STATUS_PASSED))

	require.Equal(t, contractAccAddr(), f.bank.sentFrom,
		"the escrow lives on UniversalCallback, so the debit is against it")
}

// Report before take: reportCallbackGas releases the refund and decrements
// totalEscrowed, and only then is the slack ours. Taking first would leave the
// contract briefly holding less than it owes.
func TestSettle_ReportPrecedesBurn(t *testing.T) {
	f := SetupTest(t)
	f.ctx = f.ctx.WithBlockHeight(10)
	f.bank.sendErr = errTest // fail the take, so we can see whether the report ran

	fundedRead(t, f, "0xaa", 500, 250_000, "1000000000000000000")
	key := voteToQuorum(t, f, "0xaa", obs(0x01))
	require.NoError(t, fireTerminal(f, key, uvalidatortypes.BallotStatus_BALLOT_STATUS_PASSED))

	require.Equal(t, 1, f.evm.callsTo(types.MethodReportCallbackGas),
		"the report happens before the take")
	require.True(t, f.bank.burned.IsZero(), "and the burn did not")
}

// A failed report must not burn: the contract has not released the escrow, so
// taking from it would be taking money still owed to the funder.
func TestSettle_NoBurnIfReportFails(t *testing.T) {
	f := SetupTest(t)
	f.ctx = f.ctx.WithBlockHeight(10)

	fundedRead(t, f, "0xaa", 500, 250_000, "1000000000000000000")
	key := voteToQuorum(t, f, "0xaa", obs(0x01))

	// the fulfil succeeds, the report reverts
	f.evm.vmErrors = []string{"", "execution reverted"}
	require.NoError(t, fireTerminal(f, key, uvalidatortypes.BallotStatus_BALLOT_STATUS_PASSED))

	require.True(t, f.bank.burned.IsZero(), "nothing may be burned")
	require.True(t, f.bank.sentAmount.IsZero(), "nothing may be taken")

	ur, _ := f.k.GetUniversalRead(f.ctx, "0xaa")
	require.Equal(t, types.UniversalReadStatus_UNIVERSAL_READ_STATUS_FULFILLED, ur.Status,
		"the callback ran, so the read is still fulfilled")
	require.Contains(t, ur.ErrorMsg, "reportCallbackGas", "but the failure is visible")
}

// Settlement failing must not undo the fulfilment — the callback really ran, and
// re-running it would revert on the contract's status guard.
func TestSettle_FailureKeepsReadTerminal(t *testing.T) {
	f := SetupTest(t)
	f.ctx = f.ctx.WithBlockHeight(10)
	f.bank.burnErr = errTest

	fundedRead(t, f, "0xaa", 500, 250_000, "1000000000000000000")
	key := voteToQuorum(t, f, "0xaa", obs(0x01))
	require.NoError(t, fireTerminal(f, key, uvalidatortypes.BallotStatus_BALLOT_STATUS_PASSED))

	ur, _ := f.k.GetUniversalRead(f.ctx, "0xaa")
	require.Equal(t, types.UniversalReadStatus_UNIVERSAL_READ_STATUS_FULFILLED, ur.Status)
	require.NotEmpty(t, ur.ErrorMsg)
	require.Empty(t, collectDueBy(t, f, 999), "and it does not fall back to the sweeper")
}

// A fulfilment that never settled must not be reported or burned.
func TestSettle_SkippedWhenFulfilFails(t *testing.T) {
	f := SetupTest(t)
	f.ctx = f.ctx.WithBlockHeight(10)
	f.evm.vmErrors = []string{"execution reverted"}
	f.evm.revertData = selector("CallerIsNotUCallbackModule()")

	fundedRead(t, f, "0xaa", 500, 250_000, "1000000000000000000")
	key := voteToQuorum(t, f, "0xaa", obs(0x01))
	require.NoError(t, fireTerminal(f, key, uvalidatortypes.BallotStatus_BALLOT_STATUS_PASSED))

	require.Equal(t, 0, f.evm.callsTo(types.MethodReportCallbackGas))
	require.True(t, f.bank.burned.IsZero())
}

// ── pricing ──────────────────────────────────────────────────────────────────

// The burn is clamped to the budget even if cost somehow exceeds it, so we can
// never destroy more than the funder put in.
func TestSettle_BurnNeverExceedsBudget(t *testing.T) {
	f := SetupTest(t)
	f.ctx = f.ctx.WithBlockHeight(10)

	// budget covers the declared limit, but the receipt reports far more gas
	fundedRead(t, f, "0xaa", 500, 250_000, "250000000000000")
	f.evm.gasUsed = 10_000_000 // 1e7 × 1 gwei = 1e16, far above the 2.5e14 budget

	key := voteToQuorum(t, f, "0xaa", obs(0x01))
	require.NoError(t, fireTerminal(f, key, uvalidatortypes.BallotStatus_BALLOT_STATUS_PASSED))

	budget, _ := new(big.Int).SetString("250000000000000", 10)
	require.Equal(t, budget, f.bank.burned.BigInt(), "clamped to the budget")

	report, _ := f.evm.firstCallTo(types.MethodReportCallbackGas)
	require.Equal(t, budget, report.args[1], "and reported clamped, not raw")
}

func TestCallbackCost_PricesAtBaseFee(t *testing.T) {
	f := SetupTest(t)
	cost, err := f.k.CallbackCost(f.ctx, 100_000)
	require.NoError(t, err)
	require.Equal(t, big.NewInt(100_000_000_000_000), cost, "100k gas × 1 gwei")
}
