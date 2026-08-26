package types_test

import (
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/pushchain/push-chain-node/x/uexecutor/types"
	"github.com/stretchr/testify/require"
)

// Regression coverage for F-2026-18798 — UExecutor ValidateBasic parsed unbounded
// decimal strings before ante, and never bounded them to uint256.
//
// Two independent defects, and therefore two independent kinds of test here:
//
//   - DoS: big.Int decimal parsing is superlinear, ValidateBasic runs before the
//     ante handler on a gasless message, and the cost is paid per field. The
//     length cap is what makes the reject O(1) — only the *timing* assertions
//     below catch its removal, because BitLen still rejects the value.
//   - Silent truncation: go-ethereum's ABI encoder truncates mod 2^256 without
//     erroring, so an over-range value would execute an amount different from the
//     one signed. Only BitLen catches that — 78 nines fits inside the 80-char cap
//     but has BitLen 260.

const (
	// 2^256-1 — the largest legal uint256, exactly 78 digits, BitLen 256.
	maxUint256Dec = "115792089237316195423570985008687907853269984665640564039457584007913129639935"
	// 2^256 — one past the top, BitLen 257.
	overMaxUint256Dec = "115792089237316195423570985008687907853269984665640564039457584007913129639936"
	// dosDigits sizes the DoS input. big.Int decimal parsing is superlinear —
	// measured on the dev machine: 78 digits 24µs · 100k 9.2ms · 400k 107ms ·
	// 900k 532ms · 2M 2.6s · 3M 5.7s (the finding reports 3.353s at 900k on
	// slower hardware). 3M is chosen so that the two margins are both wide: the
	// length cap rejects it in O(1) — nanoseconds — while a parse of it overruns
	// dosBudget several times over, so removing the cap fails this test loudly.
	dosDigits = 3_000_000
	// dosBudget is deliberately generous relative to the ~nanoseconds an O(1)
	// length reject costs, so the assertion cannot flake on a loaded CI runner,
	// while still failing hard if the length cap is removed and the superlinear
	// parse comes back.
	dosBudget = time.Second
)

// nines78 is 78 characters — inside the 80-char cap — but BitLen 260. This is the
// case that proves a length cap alone is not sufficient.
func nines78() string { return strings.Repeat("9", 78) }

func hugeDecimal() string { return strings.Repeat("9", dosDigits) }

// baseValidInbound mirrors the valid FUNDS fixture used in inbound_test.go.
func baseValidInbound() types.Inbound {
	return types.Inbound{
		SourceChain: "eip155:11155111",
		TxHash:      "0x123abc",
		Sender:      "0x000000000000000000000000000000000000dead",
		Recipient:   "0x000000000000000000000000000000000000beef",
		Amount:      "1000",
		AssetAddr:   "0x000000000000000000000000000000000000cafe",
		LogIndex:    "1",
		TxType:      types.TxType_FUNDS,
	}
}

func TestValidateUint256String_Bounds(t *testing.T) {
	tests := []struct {
		name        string
		value       string
		expectError bool
		errContains string
	}{
		{name: "zero", value: "0"},
		{name: "small", value: "21000"},
		{name: "one wei", value: "1"},
		{name: "typical 1e18", value: "1000000000000000000"},
		{name: "zero padded within cap", value: strings.Repeat("0", 60) + "12345"},
		{
			name:  "max uint256 accepted",
			value: maxUint256Dec,
		},
		{
			name:        "2^256 rejected",
			value:       overMaxUint256Dec,
			expectError: true,
			errContains: "exceeds the uint256 range",
		},
		{
			name:        "78 nines rejected despite fitting the length cap",
			value:       nines78(),
			expectError: true,
			errContains: "exceeds the uint256 range",
		},
		{
			name:        "negative rejected",
			value:       "-1",
			expectError: true,
			errContains: "test field must be valid",
		},
		{
			name:        "non-numeric rejected",
			value:       "not-a-number",
			expectError: true,
			errContains: "test field must be valid",
		},
		{
			name:        "decimal point rejected",
			value:       "12.34",
			expectError: true,
			errContains: "test field must be valid",
		},
		{
			name:        "over length cap rejected",
			value:       strings.Repeat("1", types.MaxUint256DecimalLen+1),
			expectError: true,
			errContains: "exceeds the maximum of 80 characters",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			bi, err := types.ValidateUint256String(tc.value, "test field must be valid")

			if tc.expectError {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.errContains)
				require.Nil(t, bi)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, bi)
			expected, ok := new(big.Int).SetString(tc.value, 10)
			require.True(t, ok)
			require.Zero(t, bi.Cmp(expected))
		})
	}
}

// The boundary pair, stated explicitly: max uint256 in, one past it out.
func TestValidateUint256String_BitLenBoundary(t *testing.T) {
	max, ok := new(big.Int).SetString(maxUint256Dec, 10)
	require.True(t, ok)
	require.Equal(t, 256, max.BitLen(), "sanity: max uint256 is 256 bits")

	over, ok := new(big.Int).SetString(overMaxUint256Dec, 10)
	require.True(t, ok)
	require.Equal(t, 257, over.BitLen(), "sanity: 2^256 is 257 bits")

	nines, ok := new(big.Int).SetString(nines78(), 10)
	require.True(t, ok)
	require.Equal(t, 260, nines.BitLen(), "sanity: 78 nines is 260 bits")
	require.LessOrEqual(t, len(nines78()), types.MaxUint256DecimalLen,
		"sanity: 78 nines fits the length cap, so only BitLen can reject it")

	_, err := types.ValidateUint256String(maxUint256Dec, "amount must be valid")
	require.NoError(t, err, "2^256-1 must be accepted")

	_, err = types.ValidateUint256String(overMaxUint256Dec, "amount must be valid")
	require.Error(t, err, "2^256 must be rejected")

	_, err = types.ValidateUint256String(nines78(), "amount must be valid")
	require.Error(t, err, "78 nines must be rejected")
}

// F-2026-18798, DoS half. A multi-million-digit field must be rejected, and
// rejected fast. The timing bound is asserted first and on purpose: it is the only
// assertion that fails if the length cap is dropped, because BitLen still rejects
// the value — just after paying for the parse.
func TestUniversalPayload_ValidateBasic_RejectsHugeDecimalFast(t *testing.T) {
	huge := hugeDecimal()

	// Every numeric field is reachable, and in the real message the attacker pays
	// for none of them — ValidateBasic runs before ante on a gasless msg.
	fields := []struct {
		name    string
		payload types.UniversalPayload
	}{
		{"value", types.UniversalPayload{To: mockHexAddress(), Value: huge}},
		{"gas_limit", types.UniversalPayload{To: mockHexAddress(), GasLimit: huge}},
		{"max_fee_per_gas", types.UniversalPayload{To: mockHexAddress(), MaxFeePerGas: huge}},
		{"max_priority_fee_per_gas", types.UniversalPayload{To: mockHexAddress(), MaxPriorityFeePerGas: huge}},
		{"nonce", types.UniversalPayload{To: mockHexAddress(), Nonce: huge}},
		{"deadline", types.UniversalPayload{To: mockHexAddress(), Deadline: huge}},
	}

	for _, f := range fields {
		t.Run(f.name, func(t *testing.T) {
			start := time.Now()
			err := f.payload.ValidateBasic()
			elapsed := time.Since(start)

			require.Error(t, err, "%s: a %d-digit value must be rejected", f.name, dosDigits)
			require.Less(t, elapsed, dosBudget,
				"%s: rejecting a %d-digit value took %s — the length cap must reject before big.Int parses",
				f.name, dosDigits, elapsed)
			require.Contains(t, err.Error(), "exceeds the maximum of 80 characters",
				"%s: must be rejected on length, before the parse", f.name)
		})
	}
}

// Same DoS shape at the other two call sites.
func TestInboundAndOutbound_RejectHugeDecimalFast(t *testing.T) {
	huge := hugeDecimal()

	t.Run("inbound amount", func(t *testing.T) {
		ib := baseValidInbound()
		ib.Amount = huge

		start := time.Now()
		err := ib.ValidateForExecution()
		elapsed := time.Since(start)

		require.Error(t, err)
		require.Less(t, elapsed, dosBudget, "rejecting a %d-digit amount took %s", dosDigits, elapsed)
		require.Contains(t, err.Error(), "exceeds the maximum of 80 characters")
	})

	t.Run("outbound amount", func(t *testing.T) {
		ob := baseValidOutbound()
		ob.Amount = huge

		start := time.Now()
		err := ob.ValidateBasic()
		elapsed := time.Since(start)

		require.Error(t, err)
		require.Less(t, elapsed, dosBudget, "rejecting a %d-digit amount took %s", dosDigits, elapsed)
		require.Contains(t, err.Error(), "exceeds the maximum of 80 characters")
	})
}

// F-2026-18798, truncation half, per call site. go-ethereum packs an over-range
// value mod 2^256 without erroring, so these must never reach the encoder.
func TestUniversalPayload_ValidateBasic_Uint256Range(t *testing.T) {
	tests := []struct {
		name        string
		payload     types.UniversalPayload
		expectError bool
	}{
		{
			name:    "max uint256 value accepted",
			payload: types.UniversalPayload{To: mockHexAddress(), Value: maxUint256Dec},
		},
		{
			name: "max uint256 on every field accepted",
			payload: types.UniversalPayload{
				To:                   mockHexAddress(),
				Value:                maxUint256Dec,
				GasLimit:             maxUint256Dec,
				MaxFeePerGas:         maxUint256Dec,
				MaxPriorityFeePerGas: maxUint256Dec,
				Nonce:                maxUint256Dec,
				Deadline:             maxUint256Dec,
			},
		},
		{
			name:    "empty numeric fields still skipped",
			payload: types.UniversalPayload{To: mockHexAddress()},
		},
		{
			name:        "2^256 value rejected",
			payload:     types.UniversalPayload{To: mockHexAddress(), Value: overMaxUint256Dec},
			expectError: true,
		},
		{
			name:        "78 nines value rejected",
			payload:     types.UniversalPayload{To: mockHexAddress(), Value: nines78()},
			expectError: true,
		},
		{
			name:        "78 nines gas_limit rejected",
			payload:     types.UniversalPayload{To: mockHexAddress(), GasLimit: nines78()},
			expectError: true,
		},
		{
			name:        "78 nines nonce rejected",
			payload:     types.UniversalPayload{To: mockHexAddress(), Nonce: nines78()},
			expectError: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.payload.ValidateBasic()
			if tc.expectError {
				require.Error(t, err)
				require.Contains(t, err.Error(), "exceeds the uint256 range")
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestInbound_ValidateForExecution_Uint256Range(t *testing.T) {
	tests := []struct {
		name        string
		amount      string
		expectError bool
	}{
		{name: "normal amount accepted", amount: "1000"},
		{name: "max uint256 accepted", amount: maxUint256Dec},
		{name: "2^256 rejected", amount: overMaxUint256Dec, expectError: true},
		{name: "78 nines rejected", amount: nines78(), expectError: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ib := baseValidInbound()
			ib.Amount = tc.amount

			err := ib.ValidateForExecution()
			if tc.expectError {
				require.Error(t, err)
				require.Contains(t, err.Error(), "exceeds the uint256 range")
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestOutboundTx_ValidateBasic_Uint256Range(t *testing.T) {
	tests := []struct {
		name        string
		amount      string
		expectError bool
		errContains string
	}{
		{name: "normal amount accepted", amount: "1000"},
		{name: "max uint256 accepted", amount: maxUint256Dec},
		{name: "2^256 rejected", amount: overMaxUint256Dec, expectError: true, errContains: "exceeds the uint256 range"},
		{name: "78 nines rejected", amount: nines78(), expectError: true, errContains: "exceeds the uint256 range"},
		// Pre-existing semantics preserved: this site requires strictly positive.
		{name: "zero still rejected", amount: "0", expectError: true, errContains: "amount must be a valid positive uint256"},
		{name: "negative still rejected", amount: "-1", expectError: true, errContains: "amount must be a valid positive uint256"},
		{name: "non-numeric still rejected", amount: "abc", expectError: true, errContains: "amount must be a valid positive uint256"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ob := baseValidOutbound()
			ob.Amount = tc.amount

			err := ob.ValidateBasic()
			if tc.expectError {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.errContains)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
