package types

import (
	"cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

var (
	_ sdk.Msg = &MsgAddChainConfig{}
)

// MsgAddChainConfig creates new instance of MsgAddChainConfig
func NewMsgAddChainConfig(
	sender sdk.Address,
	chain_config *ChainConfig,
) *MsgAddChainConfig {
	return &MsgAddChainConfig{
		Signer:      sender.String(),
		ChainConfig: chain_config,
	}
}

// Route returns the name of the module
func (msg MsgAddChainConfig) Route() string { return ModuleName }

// Type returns the the action
func (msg MsgAddChainConfig) Type() string { return "add_chain_config" }

// GetSignBytes implements the LegacyMsg interface.
func (msg MsgAddChainConfig) GetSignBytes() []byte {
	return sdk.MustSortJSON(AminoCdc.MustMarshalJSON(&msg))
}

// GetSigners returns the expected signers for a MsgAddChainConfig message.
func (msg *MsgAddChainConfig) GetSigners() []sdk.AccAddress {
	addr, _ := sdk.AccAddressFromBech32(msg.Signer)
	return []sdk.AccAddress{addr}
}

// ValidateBasic does a sanity check on the provided data.
func (msg *MsgAddChainConfig) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(msg.Signer); err != nil {
		return errors.Wrap(err, "invalid signer address")
	}

	return msg.ChainConfig.ValidateBasic()
}
