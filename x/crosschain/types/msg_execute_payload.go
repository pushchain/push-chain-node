package types

import (
	"cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

var (
	_ sdk.Msg = &MsgExecutePayload{}
)

// NewMsgExecutePayload creates new instance of MsgExecutePayload
func NewMsgExecutePayload(
	sender sdk.Address,
	caipString string,
	crosschain_payload *CrossChainPayload,
	signature []byte,
) *MsgExecutePayload {
	return &MsgExecutePayload{
		Signer:            sender.String(),
		CaipString:        caipString,
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
func (msg *MsgExecutePayload) Validate() error {
	if _, err := sdk.AccAddressFromBech32(msg.Signer); err != nil {
		return errors.Wrap(err, "invalid authority address")
	}

	// TODO: sanity check
	return nil
}
