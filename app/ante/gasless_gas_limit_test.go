package ante_test

import (
	"context"
	"errors"
	"math"
	"testing"

	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/x/authz"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/app/ante"
	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
)

// ---------------------------------------------------------------------------
// GaslessGasLimitDecorator — F-2026-18144
//
// Gasless txs pay no fee, so the fee is not a bound on the gas they declare,
// and the declared gas is what accumulates into the block's cumulative gas
// wanted. These tests pin the cap that replaces the missing economic bound.
// ---------------------------------------------------------------------------

// mockGaslessParamsKeeper satisfies ante.GaslessParamsKeeper.
type mockGaslessParamsKeeper struct {
	params uexecutortypes.Params
	err    error
	calls  int
}

func (m *mockGaslessParamsKeeper) GetParams(_ context.Context) (uexecutortypes.Params, error) {
	m.calls++
	if m.err != nil {
		return uexecutortypes.Params{}, m.err
	}
	return m.params, nil
}

func paramsWithCap(cap uint64) *mockGaslessParamsKeeper {
	return &mockGaslessParamsKeeper{params: uexecutortypes.Params{SomeValue: true, MaxGaslessTxGas: cap}}
}

// gaslessTx returns a tx whose only msg is on the IsGaslessTx allowlist.
func gaslessTx(gas uint64) mockFeeTx {
	return mockFeeTx{
		msgs:     []sdk.Msg{&uexecutortypes.MsgVoteInbound{}},
		gas:      gas,
		fee:      sdk.NewCoins(),
		feePayer: sdk.AccAddress([]byte("payer")),
	}
}

// runDecorator returns (nextCalled, err).
func runDecorator(t *testing.T, pk ante.GaslessParamsKeeper, tx sdk.Tx, simulate bool) (bool, error) {
	t.Helper()
	ggd := ante.NewGaslessGasLimitDecorator(pk)
	ctx := newAnteTestCtx(t, false)
	nextCalled := false
	_, err := ggd.AnteHandle(ctx, tx, simulate, func(ctx sdk.Context, tx sdk.Tx, simulate bool) (sdk.Context, error) {
		nextCalled = true
		return ctx, nil
	})
	return nextCalled, err
}

// TestGaslessGasLimit_AboveDefaultCapRejected is the core regression: a gasless
// tx declaring more than the cap must not reach the rest of the ante chain, so
// its declared gas is never added to the block's cumulative gas wanted.
func TestGaslessGasLimit_AboveDefaultCapRejected(t *testing.T) {
	pk := paramsWithCap(uexecutortypes.DefaultMaxGaslessTxGas)

	nextCalled, err := runDecorator(t, pk, gaslessTx(uexecutortypes.DefaultMaxGaslessTxGas+1), false)

	require.False(t, nextCalled, "over-cap gasless tx must not reach the next decorator")
	require.Error(t, err)
	require.True(t, sdkerrors.ErrInvalidGasLimit.Is(err), "expected ErrInvalidGasLimit, got: %v", err)
	require.Contains(t, err.Error(), "100000001")
	require.Contains(t, err.Error(), "100000000")
}

// TestGaslessGasLimit_AtCapAccepted pins the boundary: exactly the cap passes.
func TestGaslessGasLimit_AtCapAccepted(t *testing.T) {
	pk := paramsWithCap(uexecutortypes.DefaultMaxGaslessTxGas)

	nextCalled, err := runDecorator(t, pk, gaslessTx(uexecutortypes.DefaultMaxGaslessTxGas), false)

	require.True(t, nextCalled, "gasless tx at exactly the cap must be accepted")
	require.NoError(t, err)
}

// TestGaslessGasLimit_BelowCapAccepted covers the ordinary case.
func TestGaslessGasLimit_BelowCapAccepted(t *testing.T) {
	pk := paramsWithCap(uexecutortypes.DefaultMaxGaslessTxGas)

	nextCalled, err := runDecorator(t, pk, gaslessTx(200_000), false)

	require.True(t, nextCalled)
	require.NoError(t, err)
}

// TestGaslessGasLimit_MaxInt64Rejected is the shape from the finding: two txs
// each declaring MaxInt64 sum past what the fee market EndBlock can convert.
func TestGaslessGasLimit_MaxInt64Rejected(t *testing.T) {
	pk := paramsWithCap(uexecutortypes.DefaultMaxGaslessTxGas)

	nextCalled, err := runDecorator(t, pk, gaslessTx(math.MaxInt64), false)

	require.False(t, nextCalled, "MaxInt64 gasless tx must not reach the next decorator")
	require.Error(t, err)
	require.True(t, sdkerrors.ErrInvalidGasLimit.Is(err), "expected ErrInvalidGasLimit, got: %v", err)
}

// TestGaslessGasLimit_NonGaslessTxNotCapped proves the cap is scoped to
// fee-exempt txs: a fee-paying tx is bounded by its own fee, not by this cap,
// and the params are not even read for it.
func TestGaslessGasLimit_NonGaslessTxNotCapped(t *testing.T) {
	pk := paramsWithCap(uexecutortypes.DefaultMaxGaslessTxGas)

	tx := mockFeeTx{
		msgs:     []sdk.Msg{&banktypes.MsgSend{}},
		gas:      math.MaxInt64,
		fee:      sdk.NewCoins(sdk.NewInt64Coin("upc", 1)),
		feePayer: sdk.AccAddress([]byte("payer")),
	}

	nextCalled, err := runDecorator(t, pk, tx, false)

	require.True(t, nextCalled, "fee-paying tx must not be capped here")
	require.Equal(t, 0, pk.calls, "params must not be read for a non-gasless tx")
	require.NoError(t, err)
}

// TestGaslessGasLimit_AuthzExecVoteShape uses the exact wire shape the universal
// validators send: an authz.MsgExec wrapping a vote. This is what declared the
// hardcoded 500,000,000 on donut.
func TestGaslessGasLimit_AuthzExecVoteShape(t *testing.T) {
	inner, err := codectypes.NewAnyWithValue(&uexecutortypes.MsgVoteInbound{})
	require.NoError(t, err)

	execTx := func(gas uint64) mockFeeTx {
		return mockFeeTx{
			msgs:     []sdk.Msg{&authz.MsgExec{Grantee: "push1grantee", Msgs: []*codectypes.Any{inner}}},
			gas:      gas,
			fee:      sdk.NewCoins(),
			feePayer: sdk.AccAddress([]byte("payer")),
		}
	}

	t.Run("500M vote is rejected", func(t *testing.T) {
		pk := paramsWithCap(uexecutortypes.DefaultMaxGaslessTxGas)

		nextCalled, err := runDecorator(t, pk, execTx(500_000_000), false)

		require.False(t, nextCalled, "500M MsgExec vote must not reach the next decorator")
		require.Error(t, err)
		require.True(t, sdkerrors.ErrInvalidGasLimit.Is(err), "expected ErrInvalidGasLimit, got: %v", err)
	})

	t.Run("100M vote is accepted", func(t *testing.T) {
		pk := paramsWithCap(uexecutortypes.DefaultMaxGaslessTxGas)

		nextCalled, err := runDecorator(t, pk, execTx(100_000_000), false)

		require.True(t, nextCalled, "100M MsgExec vote must be accepted")
		require.NoError(t, err)
	})
}

// TestGaslessGasLimit_GovernanceParamTakesEffect proves the cap is the
// governance parameter and not a compiled-in constant: it must bind both
// tighter and looser than the default.
func TestGaslessGasLimit_GovernanceParamTakesEffect(t *testing.T) {
	t.Run("lowered cap binds below the default", func(t *testing.T) {
		pk := paramsWithCap(30_000_000)

		acceptedNext, acceptedErr := runDecorator(t, pk, gaslessTx(30_000_000), false)
		require.True(t, acceptedNext, "tx at the lowered cap must be accepted")
		require.NoError(t, acceptedErr)

		rejectedNext, rejectedErr := runDecorator(t, pk, gaslessTx(30_000_001), false)
		require.False(t, rejectedNext, "tx above the lowered cap must be rejected")
		require.Error(t, rejectedErr)
		require.Contains(t, rejectedErr.Error(), "30000000")
	})

	t.Run("raised cap admits gas the default would reject", func(t *testing.T) {
		pk := paramsWithCap(500_000_000)

		nextCalled, err := runDecorator(t, pk, gaslessTx(400_000_000), false)

		require.True(t, nextCalled, "raised cap must admit 400M")
		require.NoError(t, err)
	})
}

// TestGaslessGasLimit_UnsetParamFallsBackToDefault: a missing parameter must
// never mean "no cap".
func TestGaslessGasLimit_UnsetParamFallsBackToDefault(t *testing.T) {
	t.Run("zero param", func(t *testing.T) {
		pk := paramsWithCap(0)

		acceptedNext, acceptedErr := runDecorator(t, pk, gaslessTx(uexecutortypes.DefaultMaxGaslessTxGas), false)
		require.True(t, acceptedNext)
		require.NoError(t, acceptedErr)

		rejectedNext, rejectedErr := runDecorator(t, pk, gaslessTx(uexecutortypes.DefaultMaxGaslessTxGas+1), false)
		require.False(t, rejectedNext, "zero param must fall back to the default cap, not disable it")
		require.Error(t, rejectedErr)
	})

	t.Run("params read failure", func(t *testing.T) {
		pk := &mockGaslessParamsKeeper{err: errors.New("collections: not found")}

		nextCalled, err := runDecorator(t, pk, gaslessTx(uexecutortypes.DefaultMaxGaslessTxGas+1), false)

		require.False(t, nextCalled, "unreadable params must fall back to the default cap, not disable it")
		require.Error(t, err)
		require.True(t, sdkerrors.ErrInvalidGasLimit.Is(err), "expected ErrInvalidGasLimit, got: %v", err)
	})
}

// TestGaslessGasLimit_SimulationIsAlsoCapped keeps simulation honest: a gas
// estimate that would be rejected on delivery must not come back clean.
func TestGaslessGasLimit_SimulationIsAlsoCapped(t *testing.T) {
	pk := paramsWithCap(uexecutortypes.DefaultMaxGaslessTxGas)

	nextCalled, err := runDecorator(t, pk, gaslessTx(uexecutortypes.DefaultMaxGaslessTxGas+1), true)

	require.False(t, nextCalled, "simulation must apply the same cap")
	require.Error(t, err)
}
