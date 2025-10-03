package types

import (
	"strings"

	"cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

var (
	_ sdk.Msg = &MsgRemoveTokenConfig{}
)

// MsgRemoveTokenConfig creates new instance of MsgRemoveTokenConfig
func NewMsgRemoveTokenConfig(
	sender sdk.Address,
	chain string,
	token_address string,
) *MsgRemoveTokenConfig {
	return &MsgRemoveTokenConfig{
		Signer:       sender.String(),
		Chain:        chain,
		TokenAddress: token_address,
	}
}

// Route returns the name of the module
func (msg MsgRemoveTokenConfig) Route() string { return ModuleName }

// Type returns the the action
func (msg MsgRemoveTokenConfig) Type() string { return "remove_token_config" }

// GetSignBytes implements the LegacyMsg interface.
func (msg MsgRemoveTokenConfig) GetSignBytes() []byte {
	return sdk.MustSortJSON(AminoCdc.MustMarshalJSON(&msg))
}

// GetSigners returns the expected signers for a MsgRemoveTokenConfig message.
func (msg *MsgRemoveTokenConfig) GetSigners() []sdk.AccAddress {
	addr, _ := sdk.AccAddressFromBech32(msg.Signer)
	return []sdk.AccAddress{addr}
}

// ValidateBasic does a sanity check on the provided data.
func (msg *MsgRemoveTokenConfig) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(msg.Signer); err != nil {
		return errors.Wrap(err, "invalid signer address")
	}

	// Validate chain is non-empty and follows CAIP-2 format
	chain := strings.TrimSpace(msg.Chain)
	if chain == "" {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "chain cannot be empty")
	}
	if !strings.Contains(chain, ":") {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "chain must be in CAIP-2 format <namespace>:<reference>")
	}

	// Validate tokenAddress
	if strings.TrimSpace(msg.TokenAddress) == "" {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "token_address cannot be empty")
	}

	return nil
}
