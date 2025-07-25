package types

import (
	"cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

var (
	_ sdk.Msg = &MsgAddTokenConfig{}
)

// MsgAddTokenConfig creates new instance of MsgAddTokenConfig
func NewMsgAddTokenConfig(
	sender sdk.Address,
	token_config *TokenConfig,
) *MsgAddTokenConfig {
	return &MsgAddTokenConfig{
		Signer:      sender.String(),
		TokenConfig: token_config,
	}
}

// Route returns the name of the module
func (msg MsgAddTokenConfig) Route() string { return ModuleName }

// Type returns the the action
func (msg MsgAddTokenConfig) Type() string { return "add_token_config" }

// GetSignBytes implements the LegacyMsg interface.
func (msg MsgAddTokenConfig) GetSignBytes() []byte {
	return sdk.MustSortJSON(AminoCdc.MustMarshalJSON(&msg))
}

// GetSigners returns the expected signers for a MsgAddTokenConfig message.
func (msg *MsgAddTokenConfig) GetSigners() []sdk.AccAddress {
	addr, _ := sdk.AccAddressFromBech32(msg.Signer)
	return []sdk.AccAddress{addr}
}

// ValidateBasic does a sanity check on the provided data.
func (msg *MsgAddTokenConfig) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(msg.Signer); err != nil {
		return errors.Wrap(err, "invalid signer address")
	}

	return msg.TokenConfig.ValidateBasic()
}
