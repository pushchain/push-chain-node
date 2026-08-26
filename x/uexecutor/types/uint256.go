package types

import (
	"math/big"

	"cosmossdk.io/errors"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

const (
	// MaxUint256Bits is the width of a Solidity uint256. Anything wider cannot be
	// ABI-encoded faithfully: go-ethereum's encoder truncates mod 2^256 *silently*,
	// so an over-range field would make the UEA execute a value different from the
	// one the user signed over.
	MaxUint256Bits = 256

	// MaxUint256DecimalLen caps the decimal string length accepted for a uint256
	// field. 2^256-1 is exactly 78 digits; 80 leaves slack for clients that
	// zero-pad. It is a cheap pre-filter, not the range check — see
	// ValidateUint256String.
	MaxUint256DecimalLen = 80
)

// ValidateUint256String parses value as a base-10 uint256 and returns it.
//
// The order of the three checks is load-bearing (audit finding F-2026-18798):
//
//  1. Length cap FIRST, before big.Int.SetString. big.Int decimal parsing is
//     superlinear in the digit count — as reported in the finding: 78 digits
//     18µs · 100k 24.2ms · 400k 486.6ms · 900k 3.353s. This runs in
//     ValidateBasic, which BaseApp executes via validateBasicTxMsgs *before* the
//     ante handler, on messages that are gasless — so the work is free and
//     unmetered to the attacker, and it is paid per field. Rejecting on len()
//     makes that O(1) instead of O(n²).
//
//  2. Parse, rejecting non-numeric and negative input (pre-existing behaviour).
//
//  3. BitLen() <= 256. This is the authoritative range check and the one that
//     closes the silent-truncation gap. The length cap alone is NOT sufficient:
//     78 nines is only 78 characters but has BitLen 260, i.e. it fits the cap
//     and still overflows uint256.
//
// errMsg is the caller's message for a malformed or negative value, so each call
// site keeps its own wording; the two range failures append a specific reason.
func ValidateUint256String(value string, errMsg string) (*big.Int, error) {
	// 1. Cheap reject before the expensive parse.
	if len(value) > MaxUint256DecimalLen {
		return nil, errors.Wrapf(sdkerrors.ErrInvalidRequest,
			"%s: length %d exceeds the maximum of %d characters", errMsg, len(value), MaxUint256DecimalLen)
	}

	// 2. Parse.
	bi, ok := new(big.Int).SetString(value, 10)
	if !ok || bi.Sign() < 0 {
		return nil, errors.Wrap(sdkerrors.ErrInvalidRequest, errMsg)
	}

	// 3. Authoritative uint256 range check.
	if bi.BitLen() > MaxUint256Bits {
		return nil, errors.Wrapf(sdkerrors.ErrInvalidRequest,
			"%s: value exceeds the uint256 range", errMsg)
	}

	return bi, nil
}
