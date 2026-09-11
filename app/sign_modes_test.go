package app

import (
	"testing"

	"github.com/stretchr/testify/require"

	signingtype "cosmossdk.io/api/cosmos/tx/signing/v1beta1"
	"github.com/cosmos/cosmos-sdk/x/auth/tx"
)

// Regression test for F-2026-18784 (uppercase bech32 fee payer bypasses the
// DIRECT_AUX fee-payer guard).
//
// The handler in cosmossdk.io/x/tx compares the fee payer against the signer
// with a raw string compare:
//
//	if feePayer == signerData.Address { ... unauthorized ... }
//
// BIP-173 allows an all-uppercase bech32 encoding of the same account, so an
// uppercase Fee.Payer aliasing the victim's lowercase signer address slips past
// that check while everything downstream decodes both to the same AccAddress.
// Our pinned x/tx v0.14.0 still has the raw compare, so the mode stays off.
//
// This test exists so that a future refactor cannot quietly re-enable DIRECT_AUX
// by going back to appending to tx.DefaultSignModes, which contains it.
func TestEnabledSignModes_ExcludesDirectAux(t *testing.T) {
	// setup() constructs the app without InitChain, which is all this needs.
	// Setup() is avoided on purpose: it passes the "testing" chain ID and panics
	// in the EVM configurator unless another test has already initialised it.
	gapp, _ := setup(t, ChainID, false, 0)
	modes := gapp.TxConfig().SignModeHandler().SupportedModes()

	for _, m := range modes {
		require.NotEqual(t, signingtype.SignMode_SIGN_MODE_DIRECT_AUX, m,
			"SIGN_MODE_DIRECT_AUX must stay disabled until x/tx compares decoded "+
				"bytes rather than raw strings (F-2026-18784)")
	}
}

// TestEnabledSignModes_KeepsTheModesWeActuallyUse guards the other direction:
// dropping DIRECT_AUX must not take anything else with it. The universal client
// signs with SIGN_MODE_DIRECT, and TEXTUAL is enabled deliberately (it is not in
// tx.DefaultSignModes and needs the bank keeper).
func TestEnabledSignModes_KeepsTheModesWeActuallyUse(t *testing.T) {
	gapp, _ := setup(t, ChainID, false, 0)
	modes := gapp.TxConfig().SignModeHandler().SupportedModes()

	has := func(want signingtype.SignMode) bool {
		for _, m := range modes {
			if m == want {
				return true
			}
		}
		return false
	}

	require.True(t, has(signingtype.SignMode_SIGN_MODE_DIRECT), "DIRECT is what the universal client signs with")
	require.True(t, has(signingtype.SignMode_SIGN_MODE_LEGACY_AMINO_JSON), "AMINO_JSON is needed for ledger/legacy clients")
	require.True(t, has(signingtype.SignMode_SIGN_MODE_TEXTUAL), "TEXTUAL is enabled deliberately")
}

// TestDefaultSignModesStillContainsDirectAux documents why the explicit list
// exists. If upstream ever drops DIRECT_AUX from DefaultSignModes this test
// fails, and the explicit enumeration can be reconsidered.
func TestDefaultSignModesStillContainsDirectAux(t *testing.T) {
	found := false
	for _, m := range tx.DefaultSignModes {
		if m.String() == "SIGN_MODE_DIRECT_AUX" {
			found = true
		}
	}
	require.True(t, found,
		"tx.DefaultSignModes no longer contains DIRECT_AUX; the explicit list in app.go may no longer be needed")
}
