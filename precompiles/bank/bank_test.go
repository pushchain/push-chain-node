package bank_test

import (
	"context"
	"encoding/json"
	"math/big"
	"reflect"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	bankprecompile "github.com/cosmos/evm/precompiles/bank"
	"github.com/ethereum/go-ethereum/accounts/abi"
	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/stretchr/testify/require"

	pushbank "github.com/pushchain/push-chain-node/precompiles/bank"
)

// recordingKeeper captures what the precompile asked the bank module to do.
type recordingKeeper struct {
	sentFrom   sdk.AccAddress
	sentTo     string
	sentCoins  sdk.Coins
	burnModule string
	burnCoins  sdk.Coins
	sendErr    error
	burnErr    error
}

func (k *recordingKeeper) SendCoinsFromAccountToModule(_ context.Context, from sdk.AccAddress, module string, amt sdk.Coins) error {
	k.sentFrom, k.sentTo, k.sentCoins = from, module, amt
	return k.sendErr
}

func (k *recordingKeeper) BurnCoins(_ context.Context, module string, amt sdk.Coins) error {
	k.burnModule, k.burnCoins = module, amt
	return k.burnErr
}

func newPrecompile(t *testing.T, k *recordingKeeper) *pushbank.Precompile {
	t.Helper()
	upstream := bankprecompile.NewPrecompile(nil, nil)
	p, err := pushbank.NewPrecompile(k, upstream)
	require.NoError(t, err)
	return p
}

// extendedABI is the ABI the precompile advertises.
func extendedABI(t *testing.T) abi.ABI {
	t.Helper()
	var parsed struct {
		ABI abi.ABI `json:"abi"`
	}
	require.NoError(t, json.Unmarshal(pushbank.ABIJSON(), &parsed))
	return parsed.ABI
}

// The extension must not shadow or drop anything upstream exposes.
func TestExtendedABI_KeepsUpstreamMethods(t *testing.T) {
	a := extendedABI(t)
	for _, m := range []string{"balances", "totalSupply", "supplyOf", "burn"} {
		_, ok := a.Methods[m]
		require.True(t, ok, "missing %s", m)
	}
	require.Len(t, a.Methods, 4, "exactly the upstream three plus burn")
}

// Registering at the upstream bank address is what makes this an extension rather
// than a second precompile.
func TestAddress_MatchesUpstreamBank(t *testing.T) {
	k := &recordingKeeper{}
	upstream := bankprecompile.NewPrecompile(nil, nil)
	p := newPrecompile(t, k)
	require.Equal(t, upstream.Address(), p.Address())
}

// THE security property: burn must be marked state-mutating, or SetupABI will let
// it run under STATICCALL — mutating state in a context the EVM guarantees is pure.
// Upstream returns false unconditionally because bank is read-only there.
func TestIsTransaction_BurnOnly(t *testing.T) {
	p := newPrecompile(t, &recordingKeeper{})
	a := extendedABI(t)

	burn := a.Methods["burn"]
	require.True(t, p.IsTransaction(&burn), "burn MUST be a transaction")

	for _, name := range []string{"balances", "totalSupply", "supplyOf"} {
		m := a.Methods[name]
		require.False(t, p.IsTransaction(&m), "%s must stay a query", name)
	}
	require.False(t, p.IsTransaction(nil), "nil method must not be treated as a write")
}

// Upstream prices against an ABI with no burn, so an un-overridden RequiredGas
// would charge nothing for it.
func TestRequiredGas_ChargesForBurn(t *testing.T) {
	p := newPrecompile(t, &recordingKeeper{})
	a := extendedABI(t)

	input, err := a.Pack("burn", big.NewInt(1))
	require.NoError(t, err)
	require.Equal(t, uint64(pushbank.BurnGas), p.RequiredGas(input))

	// short input must not panic or be mistaken for burn
	require.NotPanics(t, func() { p.RequiredGas([]byte{0x01}) })
	require.NotPanics(t, func() { p.RequiredGas(nil) })
}

// Only the burn selector is intercepted; anything else must fall through.
func TestSelectorRouting(t *testing.T) {
	p := newPrecompile(t, &recordingKeeper{})
	a := extendedABI(t)

	burnInput, err := a.Pack("burn", big.NewInt(1))
	require.NoError(t, err)
	require.Equal(t, uint64(pushbank.BurnGas), p.RequiredGas(burnInput))

	// a query selector must NOT be priced as a burn
	balInput, err := a.Pack("balances", ethcommon.HexToAddress("0x1"))
	require.NoError(t, err)
	require.NotEqual(t, uint64(pushbank.BurnGas), p.RequiredGas(balInput),
		"query selector must not route to burn")
}

// The two bank calls must target a module holding the Burner permission, and carry
// the caller's own address as the source.
func TestBurn_MovesThenBurns(t *testing.T) {
	k := &recordingKeeper{}
	p := newPrecompile(t, k)

	caller := ethcommon.HexToAddress("0x00000000000000000000000000000000000000AA")
	out, err := p.BurnForTest(sdk.Context{}, caller, big.NewInt(1_000))
	require.NoError(t, err)
	require.NotEmpty(t, out)

	require.Equal(t, sdk.AccAddress(caller.Bytes()), k.sentFrom,
		"source must be the caller, never an argument")
	require.Equal(t, "evm", k.sentTo, "must go to a module with Burner")
	require.Equal(t, k.sentCoins, k.burnCoins, "burn exactly what was moved")
	require.Equal(t, "upc", k.burnCoins[0].Denom)
	require.Equal(t, "1000", k.burnCoins[0].Amount.String())
}

func TestBurn_RejectsBadAmounts(t *testing.T) {
	for name, amt := range map[string]*big.Int{
		"zero":     big.NewInt(0),
		"negative": big.NewInt(-1),
	} {
		t.Run(name, func(t *testing.T) {
			k := &recordingKeeper{}
			p := newPrecompile(t, k)
			_, err := p.BurnForTest(sdk.Context{}, ethcommon.Address{}, amt)
			require.Error(t, err)
			require.Nil(t, k.burnCoins, "nothing may be burned")
			require.Nil(t, k.sentCoins, "nothing may be moved")
		})
	}
}

// If the transfer fails there must be no burn — otherwise the module would destroy
// coins it never received, reducing supply against someone else's balance.
func TestBurn_NoBurnIfTransferFails(t *testing.T) {
	k := &recordingKeeper{sendErr: errBoom}
	p := newPrecompile(t, k)

	_, err := p.BurnForTest(sdk.Context{}, ethcommon.HexToAddress("0x1"), big.NewInt(5))
	require.Error(t, err)
	require.Nil(t, k.burnCoins, "burn must not run after a failed transfer")
}

var errBoom = errBoomType{}

type errBoomType struct{}

func (errBoomType) Error() string { return "boom" }

// ── Override dispatch ────────────────────────────────────────────────────────
//
// Embedding promotes the upstream methods, so an override only takes effect if it
// is declared on the outer type with the exact same name and signature. A typo
// compiles fine and silently leaves the upstream behaviour in place — free burns
// (RequiredGas) or state writes under STATICCALL (IsTransaction). These assert
// through vm.PrecompiledContract, which is how the EVM actually dispatches.

var _ vm.PrecompiledContract = (*pushbank.Precompile)(nil)

func TestOverrides_WinThroughTheInterface(t *testing.T) {
	var iface vm.PrecompiledContract = newPrecompile(t, &recordingKeeper{})
	a := extendedABI(t)

	burnInput, err := a.Pack("burn", big.NewInt(1))
	require.NoError(t, err)
	balInput, err := a.Pack("balances", ethcommon.HexToAddress("0x1"))
	require.NoError(t, err)

	// RequiredGas: ours for burn, upstream's for a query
	require.Equal(t, uint64(pushbank.BurnGas), iface.RequiredGas(burnInput),
		"RequiredGas override did not win — burn would be free")
	require.NotEqual(t, uint64(pushbank.BurnGas), iface.RequiredGas(balInput),
		"query must fall through to upstream pricing")

	// Address: inherited, and must stay the upstream bank address
	require.Equal(t, bankprecompile.NewPrecompile(nil, nil).Address(), iface.Address())
}

// The failure mode these guard against is a rename: declare the override with a
// slightly different name or signature and it compiles, but the promoted upstream
// method is what the interface dispatches to. Comparing method pointers catches
// exactly that — if an override stops shadowing, ours and upstream's become the
// same function.
func TestOverrides_AreNotUpstreamsMethods(t *testing.T) {
	p := newPrecompile(t, &recordingKeeper{})
	upstream := bankprecompile.NewPrecompile(nil, nil)

	for name, pair := range map[string][2]uintptr{
		"Run": {
			reflect.ValueOf(p.Run).Pointer(),
			reflect.ValueOf(upstream.Run).Pointer(),
		},
		"RequiredGas": {
			reflect.ValueOf(p.RequiredGas).Pointer(),
			reflect.ValueOf(upstream.RequiredGas).Pointer(),
		},
		"IsTransaction": {
			reflect.ValueOf(p.IsTransaction).Pointer(),
			reflect.ValueOf(upstream.IsTransaction).Pointer(),
		},
	} {
		require.NotEqual(t, pair[1], pair[0],
			"%s is still upstream's — the override is not shadowing", name)
	}
}

// Address is deliberately NOT overridden: it must stay upstream's so the extension
// registers at the bank address rather than a new one.
func TestAddress_IsInherited(t *testing.T) {
	p := newPrecompile(t, &recordingKeeper{})
	upstream := bankprecompile.NewPrecompile(nil, nil)
	require.Equal(t,
		reflect.ValueOf(upstream.Address).Pointer(),
		reflect.ValueOf(p.Address).Pointer(),
		"Address must remain inherited")
}
