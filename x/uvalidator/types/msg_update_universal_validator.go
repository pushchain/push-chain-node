package types

import (
	"cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

var (
	_ sdk.Msg = &MsgUpdateUniversalValidator{}
)

// NewMsgUpdateUniversalValidator creates new instance of MsgUpdateUniversalValidator
func NewMsgUpdateUniversalValidator(
	sender sdk.Address,
	coreValidatorAddress sdk.Address,
	network NetworkInfo,
) *MsgUpdateUniversalValidator {
	return &MsgUpdateUniversalValidator{
		Signer:               sender.String(),
		CoreValidatorAddress: coreValidatorAddress.String(),
		Network:              &network,
	}
}

// Route returns the name of the module
func (msg MsgUpdateUniversalValidator) Route() string { return ModuleName }

// Type returns the action
func (msg MsgUpdateUniversalValidator) Type() string { return "update_universal_validator" }

// GetSignBytes implements the LegacyMsg interface.
func (msg MsgUpdateUniversalValidator) GetSignBytes() []byte {
	return sdk.MustSortJSON(AminoCdc.MustMarshalJSON(&msg))
}

// GetSigners returns the expected signers for a MsgUpdateUniversalValidator message.
func (msg *MsgUpdateUniversalValidator) GetSigners() []sdk.AccAddress {
	addr, _ := sdk.AccAddressFromBech32(msg.Signer)
	return []sdk.AccAddress{addr}
}

func (msg *MsgUpdateUniversalValidator) ValidateBasic() error {
	// Validate signer
	if _, err := sdk.AccAddressFromBech32(msg.Signer); err != nil {
		return errors.Wrap(err, "invalid signer address")
	}

	// Validate core validator address (must be a valid valoper address)
	_, err := sdk.ValAddressFromBech32(msg.CoreValidatorAddress)
	if err != nil {
		return errors.Wrap(err, "invalid core validator address")
	}

	return msg.Network.ValidateBasic()
}
