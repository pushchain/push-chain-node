package types

import (
	"cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

var (
	_ sdk.Msg = &MsgUpdateChainConfig{}
)

// MsgUpdateChainConfig creates new instance of MsgUpdateChainConfig
func NewMsgUpdateChainConfig(
	sender sdk.Address,
	chain_config *ChainConfig,
) *MsgUpdateChainConfig {
	return &MsgUpdateChainConfig{
		Signer:      sender.String(),
		ChainConfig: chain_config,
	}
}

// Route returns the name of the module
func (msg MsgUpdateChainConfig) Route() string { return ModuleName }

// Type returns the action
func (msg MsgUpdateChainConfig) Type() string { return "update_chain_config" }

// GetSignBytes implements the LegacyMsg interface.
func (msg MsgUpdateChainConfig) GetSignBytes() []byte {
	return sdk.MustSortJSON(AminoCdc.MustMarshalJSON(&msg))
}

// GetSigners returns the expected signers for a MsgUpdateChainConfig message.
func (msg *MsgUpdateChainConfig) GetSigners() []sdk.AccAddress {
	addr, _ := sdk.AccAddressFromBech32(msg.Signer)
	return []sdk.AccAddress{addr}
}

// ValidateBasic does a sanity check on the provided data.
func (msg *MsgUpdateChainConfig) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(msg.Signer); err != nil {
		return errors.Wrap(err, "invalid signer address")
	}

	return msg.ChainConfig.ValidateBasic()
}
