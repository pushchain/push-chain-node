package types

import (
	"cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

var (
	_ sdk.Msg = &MsgRemoveUniversalValidator{}
)

// NewMsgRemoveUniversalValidator creates new instance of MsgRemoveUniversalValidator
func NewMsgRemoveUniversalValidator(
	sender sdk.Address,
	universalValidatorAddress sdk.Address,
) *MsgRemoveUniversalValidator {
	return &MsgRemoveUniversalValidator{
		Signer:                    sender.String(),
		UniversalValidatorAddress: universalValidatorAddress.String(),
	}
}

// Route returns the name of the module
func (msg MsgRemoveUniversalValidator) Route() string { return ModuleName }

// Type returns the the action
func (msg MsgRemoveUniversalValidator) Type() string { return "remove_universal_validator" }

// GetSignBytes implements the LegacyMsg interface.
func (msg MsgRemoveUniversalValidator) GetSignBytes() []byte {
	return sdk.MustSortJSON(AminoCdc.MustMarshalJSON(&msg))
}

// GetSigners returns the expected signers for a MsgRemoveUniversalValidator message.
func (msg *MsgRemoveUniversalValidator) GetSigners() []sdk.AccAddress {
	addr, _ := sdk.AccAddressFromBech32(msg.Signer)
	return []sdk.AccAddress{addr}
}

func (msg *MsgRemoveUniversalValidator) ValidateBasic() error {
	// Validate signer
	if _, err := sdk.AccAddressFromBech32(msg.Signer); err != nil {
		return errors.Wrap(err, "invalid signer address")
	}

	// Validate universal validator address (must be a normal account address)
	if _, err := sdk.AccAddressFromBech32(msg.UniversalValidatorAddress); err != nil {
		return errors.Wrap(err, "invalid universal validator address")
	}

	return nil
}
