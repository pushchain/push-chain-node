package types

import (
	"cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

var (
	_ sdk.Msg = &MsgUpdateAdminParams{}
)

// NewMsgUpdateParams creates new instance of MsgUpdateParams
func NewMsgUpdateAdminParams(
	sender sdk.Address,
	factoryAddress string,
	verifierPrecompile string,
) *MsgUpdateAdminParams {
	return &MsgUpdateAdminParams{
		Admin: sender.String(),
		AdminParams: AdminParams{
			FactoryAddress:     factoryAddress,
			VerifierPrecompile: verifierPrecompile,
		},
	}
}

// Route returns the name of the module
func (msg MsgUpdateAdminParams) Route() string { return ModuleName }

// Type returns the the action
func (msg MsgUpdateAdminParams) Type() string { return "update_params" }

// GetSignBytes implements the LegacyMsg interface.
func (msg MsgUpdateAdminParams) GetSignBytes() []byte {
	return sdk.MustSortJSON(AminoCdc.MustMarshalJSON(&msg))
}

// GetSigners returns the expected signers for a MsgUpdateAdminParams message.
func (msg *MsgUpdateAdminParams) GetSigners() []sdk.AccAddress {
	addr, _ := sdk.AccAddressFromBech32(msg.Admin)
	return []sdk.AccAddress{addr}
}

// ValidateBasic does a sanity check on the provided data.
func (msg *MsgUpdateAdminParams) Validate() error {
	if _, err := sdk.AccAddressFromBech32(msg.Admin); err != nil {
		return errors.Wrap(err, "invalid admin address")
	}

	return msg.AdminParams.Validate()
}
