package types

import (
	"encoding/hex"
	"strings"

	"cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/ethereum/go-ethereum/common"
)

var (
	_ sdk.Msg = &MsgExecutePayload{}
)

// NewMsgExecutePayload creates new instance of MsgExecutePayload
func NewMsgExecutePayload(
	sender sdk.Address,
	universalAccountId *UniversalAccountId,
	universalPayload *UniversalPayload,
	verificationData string,
) *MsgExecutePayload {
	return &MsgExecutePayload{
		Signer:             sender.String(),
		UniversalAccountId: universalAccountId,
		UniversalPayload:   universalPayload,
		VerificationData:   verificationData,
	}
}

// Route returns the name of the module
func (msg MsgExecutePayload) Route() string { return ModuleName }

// Type returns the action
func (msg MsgExecutePayload) Type() string { return "execute_payload" }

// GetSignBytes implements the LegacyMsg interface.
func (msg MsgExecutePayload) GetSignBytes() []byte {
	return sdk.MustSortJSON(AminoCdc.MustMarshalJSON(&msg))
}

// GetSigners returns the expected signers for a MsgExecutePayload message.
func (msg *MsgExecutePayload) GetSigners() []sdk.AccAddress {
	addr, _ := sdk.AccAddressFromBech32(msg.Signer)
	return []sdk.AccAddress{addr}
}

// ValidateBasic does a sanity check on the provided data.
func (msg *MsgExecutePayload) ValidateBasic() error {
	// Validate signer.
	// The length check is deliberate: bech32 account addresses may carry up to
	// 255 bytes, and this signer is later converted to a 20-byte EVM address
	// that keeps only the rightmost bytes. A longer signer would therefore
	// collapse onto an unrelated EVM address, including module addresses that
	// the UEA trusts. Reject it here, at CheckTx, before the ante chain runs.
	signerBz, err := sdk.AccAddressFromBech32(msg.Signer)
	if err != nil {
		return errors.Wrap(err, "invalid signer address")
	}
	if len(signerBz) != common.AddressLength {
		return errors.Wrapf(sdkerrors.ErrInvalidAddress,
			"invalid signer address length: got %d bytes, want %d", len(signerBz), common.AddressLength)
	}

	// Validate universalAccountId
	if msg.UniversalAccountId == nil {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "universal account cannot be nil")
	}

	// Validate universal payload
	if msg.UniversalPayload == nil {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "universal payload cannot be nil")
	}

	// Validate verificationData
	if len(msg.VerificationData) == 0 {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "verificationData cannot be empty")
	}
	if _, err := hex.DecodeString(strings.TrimPrefix(msg.VerificationData, "0x")); err != nil {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "invalid verificationData hex")
	}

	// Validate universalAccountId structure
	if err := msg.UniversalAccountId.ValidateBasic(); err != nil {
		return errors.Wrap(err, "invalid universalAccountId")
	}

	// Validate universal payload structure
	if err := msg.UniversalPayload.ValidateBasic(); err != nil {
		return errors.Wrap(err, "invalid universal payload")
	}

	return nil
}
