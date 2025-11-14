package types

import (
	"cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

var (
	_ sdk.Msg = &MsgMigrateUEA{}
)

// NewMsgExecutePayload creates new instance of MsgExecutePayload
func NewMsgMigrateUEA(
	sender sdk.Address,
	universalAccountId *UniversalAccountId,
	universalPayload *UniversalPayload,
	signature string,
) *MsgExecutePayload {
	return &MsgExecutePayload{
		Signer:             sender.String(),
		UniversalAccountId: universalAccountId,
		UniversalPayload:   universalPayload,
		VerificationData:   signature,
	}
}

// Route returns the name of the module
func (msg MsgMigrateUEA) Route() string { return ModuleName }

// Type returns the action
func (msg MsgMigrateUEA) Type() string { return "execute_payload" }

// GetSignBytes implements the LegacyMsg interface.
func (msg MsgMigrateUEA) GetSignBytes() []byte {
	return sdk.MustSortJSON(AminoCdc.MustMarshalJSON(&msg))
}

// GetSigners returns the expected signers for a MsgExecutePayload message.
func (msg *MsgMigrateUEA) GetSigners() []sdk.AccAddress {
	addr, _ := sdk.AccAddressFromBech32(msg.Signer)
	return []sdk.AccAddress{addr}
}

// ValidateBasic does a sanity check on the provided data.
func (msg *MsgMigrateUEA) ValidateBasic() error {
	// Validate signer
	if _, err := sdk.AccAddressFromBech32(msg.Signer); err != nil {
		return errors.Wrap(err, "invalid signer address")
	}

	// Validate universalAccountId
	if msg.UniversalAccountId == nil {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "universal account cannot be nil")
	}

	// Validate universal payload
	if msg.UniversalPayload == nil {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "universal payload cannot be nil")
	}

	// Validate Signature
	if len(msg.Signature) == 0 {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "verificationData cannot be empty")
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
