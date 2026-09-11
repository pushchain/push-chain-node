//go:build !test
// +build !test

package app

import (
	evmtypes "github.com/cosmos/evm/x/vm/types"
)

// seedDefaultEvmCoinInfo populates the EVM coin-info fallback at app construction.
//
// The authoritative value is set by x/vm's PreBlock, but x/upgrade's PreBlocker
// runs before it. An upgrade handler that touches EVM state — reading an account,
// or writing one, which sets a balance — resolves the coin denom through a global
// that is still nil at that point, and the node dies on a nil dereference partway
// through the upgrade. That is a chain halt, not a failed tx.
//
// Upstream provides this fallback for exactly that window (and for RPC served
// before the first PreBlock); nothing in this app was populating it. It is a
// different variable from the one x/vm sets, so there is no double-set: the getter
// prefers the PreBlock value and falls back to this only while that is nil.
func seedDefaultEvmCoinInfo(coinInfo evmtypes.EvmCoinInfo) {
	evmtypes.SetDefaultEvmCoinInfo(coinInfo)
}
