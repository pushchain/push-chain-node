package types

import (
	"math/big"
	"strings"

	"cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

var _ sdk.Msg = &MsgInitiateFundMigration{}

const (
	// maxUint256Bits is the width of the native balances this message deals in.
	maxUint256Bits = 256
	// maxUint256DecimalLen bounds the decimal string before it is parsed. Max
	// uint256 is exactly 78 digits; the slack absorbs a zero-padded client value.
	// Checking length first matters: big.Int decimal parsing is superlinear, so a
	// caller-supplied million-digit string is rejected in O(1) instead of being
	// parsed and then discarded.
	maxUint256DecimalLen = 80
)

// ValidateBasic does a sanity check on the provided data.
func (msg *MsgInitiateFundMigration) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(msg.Signer); err != nil {
		return errors.Wrap(err, "invalid signer address")
	}
	if strings.TrimSpace(msg.OldKeyId) == "" {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "old_key_id is required")
	}
	if strings.TrimSpace(msg.Chain) == "" {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "chain is required")
	}
	if _, err := ParseBalance(msg.Balance); err != nil {
		return err
	}
	return nil
}

// ParseBalance validates the admin-supplied native balance and returns it.
//
// The value is the balance the admin observed on the old TSS address, not the
// amount to sweep: the keeper derives that as balance - gas - l1_gas_fee using
// the fee figures it fetches itself, so the emitted transfer_amount is
// consistent with the emitted fees by construction. The admin cannot compute it
// because those fees are read from UniversalCore inside the handler, after the
// message is signed.
func ParseBalance(balance string) (*big.Int, error) {
	balance = strings.TrimSpace(balance)
	if balance == "" {
		return nil, errors.Wrap(sdkerrors.ErrInvalidRequest, "balance is required")
	}
	if len(balance) > maxUint256DecimalLen {
		return nil, errors.Wrapf(sdkerrors.ErrInvalidRequest,
			"balance is too long: %d characters (max %d)", len(balance), maxUint256DecimalLen)
	}
	bi, ok := new(big.Int).SetString(balance, 10)
	if !ok || bi.Sign() < 0 {
		return nil, errors.Wrap(sdkerrors.ErrInvalidRequest, "balance must be a valid non-negative integer")
	}
	// A length cap alone is not enough: 78 nines fits in 78 characters but is
	// wider than uint256, and the EVM ABI encoder truncates such values silently.
	if bi.BitLen() > maxUint256Bits {
		return nil, errors.Wrap(sdkerrors.ErrInvalidRequest, "balance exceeds uint256")
	}
	return bi, nil
}
