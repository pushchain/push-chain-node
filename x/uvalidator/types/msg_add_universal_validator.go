package types

import (
	"cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

var (
	_ sdk.Msg = &MsgAddUniversalValidator{}
)

// NewMsgAddUniversalValidator creates new instance of MsgAddUniversalValidator
func NewMsgAddUniversalValidator(
	sender sdk.Address,
	coreValidatorAddress sdk.Address,
	pubKey string,
	network NetworkInfo,
) *MsgAddUniversalValidator {
	return &MsgAddUniversalValidator{
		Signer:               sender.String(),
		CoreValidatorAddress: coreValidatorAddress.String(),
		Network:              &network,
	}
}

// Route returns the name of the module
func (msg MsgAddUniversalValidator) Route() string { return ModuleName }

// Type returns the action
func (msg MsgAddUniversalValidator) Type() string { return "add_universal_validator" }

// GetSignBytes implements the LegacyMsg interface.
func (msg MsgAddUniversalValidator) GetSignBytes() []byte {
	return sdk.MustSortJSON(AminoCdc.MustMarshalJSON(&msg))
}

// GetSigners returns the expected signers for a MsgAddUniversalValidator message.
func (msg *MsgAddUniversalValidator) GetSigners() []sdk.AccAddress {
	addr, _ := sdk.AccAddressFromBech32(msg.Signer)
	return []sdk.AccAddress{addr}
}

func (msg *MsgAddUniversalValidator) ValidateBasic() error {
	// Validate signer
	if _, err := sdk.AccAddressFromBech32(msg.Signer); err != nil {
		return errors.Wrap(err, "invalid signer address")
	}

	// Validate core validator address (must be a valid valoper address)
	_, err := sdk.ValAddressFromBech32(msg.CoreValidatorAddress)
	if err != nil {
		return errors.Wrap(err, "invalid core validator address")
	}

	if msg.Network == nil {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "network info is required")
	}

	return msg.Network.ValidateBasic()
}
