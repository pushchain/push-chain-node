package types

import (
	"cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

var (
	_ sdk.Msg = &MsgMintPC{}
)

// NewMsgMintPC creates new instance of MsgMintPC
func NewMsgMintPC(
	sender sdk.Address,
	universalAccount *UniversalAccount,
	txHash string,
) *MsgMintPC {
	return &MsgMintPC{
		Signer:           sender.String(),
		UniversalAccount: universalAccount,
		TxHash:           txHash,
	}
}

// Route returns the name of the module
func (msg MsgMintPC) Route() string { return ModuleName }

// Type returns the the action
func (msg MsgMintPC) Type() string { return "mint_pc" }

// GetSignBytes implements the LegacyMsg interface.
func (msg MsgMintPC) GetSignBytes() []byte {
	return sdk.MustSortJSON(AminoCdc.MustMarshalJSON(&msg))
}

// GetSigners returns the expected signers for a MsgMintPC message.
func (msg *MsgMintPC) GetSigners() []sdk.AccAddress {
	addr, _ := sdk.AccAddressFromBech32(msg.Signer)
	return []sdk.AccAddress{addr}
}

// ValidateBasic does a sanity check on the provided data.
func (msg *MsgMintPC) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(msg.Signer); err != nil {
		return errors.Wrap(err, "invalid signer address")
	}

	// Validate universalAccount
	if msg.UniversalAccount == nil {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "universalAccount cannot be nil")
	}

	// Validate txHash
	if len(msg.TxHash) == 0 {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "txHash cannot be empty")
	}

	return msg.UniversalAccount.ValidateBasic()
}
