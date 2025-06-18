package types

import (
	"cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

var (
	_ sdk.Msg = &MsgMintPush{}
)

// NewMsgMintPush creates new instance of MsgMintPush
func NewMsgMintPush(
	sender sdk.Address,
	universalAccount *UniversalAccount,
	txHash string,
) *MsgMintPush {
	return &MsgMintPush{
		Signer:           sender.String(),
		UniversalAccount: universalAccount,
		TxHash:           txHash,
	}
}

// Route returns the name of the module
func (msg MsgMintPush) Route() string { return ModuleName }

// Type returns the the action
func (msg MsgMintPush) Type() string { return "mint_push" }

// GetSignBytes implements the LegacyMsg interface.
func (msg MsgMintPush) GetSignBytes() []byte {
	return sdk.MustSortJSON(AminoCdc.MustMarshalJSON(&msg))
}

// GetSigners returns the expected signers for a MsgMintPush message.
func (msg *MsgMintPush) GetSigners() []sdk.AccAddress {
	addr, _ := sdk.AccAddressFromBech32(msg.Signer)
	return []sdk.AccAddress{addr}
}

// ValidateBasic does a sanity check on the provided data.
func (msg *MsgMintPush) ValidateBasic() error {
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
