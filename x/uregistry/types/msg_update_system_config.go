package types

import (
	"cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

var (
	_ sdk.Msg = &MsgUpdateSystemConfig{}
)

// MsgUpdateSystemConfig creates new instance of MsgUpdateSystemConfig
func NewMsgUpdateSystemConfig(
	sender sdk.Address,
	system_config *SystemConfig,
) *MsgUpdateSystemConfig {
	return &MsgUpdateSystemConfig{
		Signer:       sender.String(),
		SystemConfig: system_config,
	}
}

// Route returns the name of the module
func (msg MsgUpdateSystemConfig) Route() string { return ModuleName }

// Type returns the the action
func (msg MsgUpdateSystemConfig) Type() string { return "update_system_config" }

// GetSignBytes implements the LegacyMsg interface.
func (msg MsgUpdateSystemConfig) GetSignBytes() []byte {
	return sdk.MustSortJSON(AminoCdc.MustMarshalJSON(&msg))
}

// GetSigners returns the expected signers for a MsgUpdateSystemConfig message.
func (msg *MsgUpdateSystemConfig) GetSigners() []sdk.AccAddress {
	addr, _ := sdk.AccAddressFromBech32(msg.Signer)
	return []sdk.AccAddress{addr}
}

// ValidateBasic does a sanity check on the provided data.
func (msg *MsgUpdateSystemConfig) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(msg.Signer); err != nil {
		return errors.Wrap(err, "invalid signer address")
	}

	return msg.SystemConfig.ValidateBasic()
}
