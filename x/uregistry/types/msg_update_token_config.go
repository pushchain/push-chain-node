package types

import (
	"cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

var (
	_ sdk.Msg = &MsgUpdateTokenConfig{}
)

// MsgUpdateTokenConfig creates new instance of MsgUpdateTokenConfig
func NewMsgUpdateTokenConfig(
	sender sdk.Address,
	token_config *TokenConfig,
) *MsgUpdateTokenConfig {
	return &MsgUpdateTokenConfig{
		Signer:      sender.String(),
		TokenConfig: token_config,
	}
}

// Route returns the name of the module
func (msg MsgUpdateTokenConfig) Route() string { return ModuleName }

// Type returns the the action
func (msg MsgUpdateTokenConfig) Type() string { return "update_token_config" }

// GetSignBytes implements the LegacyMsg interface.
func (msg MsgUpdateTokenConfig) GetSignBytes() []byte {
	return sdk.MustSortJSON(AminoCdc.MustMarshalJSON(&msg))
}

// GetSigners returns the expected signers for a MsgUpdateTokenConfig message.
func (msg *MsgUpdateTokenConfig) GetSigners() []sdk.AccAddress {
	addr, _ := sdk.AccAddressFromBech32(msg.Signer)
	return []sdk.AccAddress{addr}
}

// ValidateBasic does a sanity check on the provided data.
func (msg *MsgUpdateTokenConfig) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(msg.Signer); err != nil {
		return errors.Wrap(err, "invalid signer address")
	}

	return msg.TokenConfig.ValidateBasic()
}
