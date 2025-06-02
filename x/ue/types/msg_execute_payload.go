package types

import (
	"encoding/hex"
	"strings"

	"cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

var (
	_ sdk.Msg = &MsgExecutePayload{}
)

// NewMsgExecutePayload creates new instance of MsgExecutePayload
func NewMsgExecutePayload(
	sender sdk.Address,
	accountId *AccountId,
	crosschain_payload *CrossChainPayload,
	signature string,
) *MsgExecutePayload {
	return &MsgExecutePayload{
		Signer:            sender.String(),
		AccountId:         accountId,
		CrosschainPayload: crosschain_payload,
		Signature:         signature,
	}
}

// Route returns the name of the module
func (msg MsgExecutePayload) Route() string { return ModuleName }

// Type returns the the action
func (msg MsgExecutePayload) Type() string { return "deploy_nmsc" }

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
	// Validate signer
	if _, err := sdk.AccAddressFromBech32(msg.Signer); err != nil {
		return errors.Wrap(err, "invalid signer address")
	}

	// Validate accountId
	if msg.AccountId == nil {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "accountId cannot be nil")
	}

	// Validate crosschain payload
	if msg.CrosschainPayload == nil {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "crosschain payload cannot be nil")
	}

	// Validate signature
	if len(msg.Signature) == 0 {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "signature cannot be empty")
	}
	if _, err := hex.DecodeString(strings.TrimPrefix(msg.Signature, "0x")); err != nil {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "invalid signature hex")
	}

	// Validate accountId structure
	if err := msg.AccountId.ValidateBasic(); err != nil {
		return errors.Wrap(err, "invalid accountId")
	}

	// Validate crosschain payload structure
	if err := msg.CrosschainPayload.ValidateBasic(); err != nil {
		return errors.Wrap(err, "invalid crosschain payload")
	}

	return nil
}
