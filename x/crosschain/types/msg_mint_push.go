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
	txHash string,
	caipString string,
) *MsgMintPush {
	return &MsgMintPush{
		Signer:     sender.String(),
		TxHash:     txHash,
		CaipString: caipString,
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
func (msg *MsgMintPush) Validate() error {
	if _, err := sdk.AccAddressFromBech32(msg.Signer); err != nil {
		return errors.Wrap(err, "invalid signer address")
	}

	// Validate caipString
	if len(msg.CaipString) == 0 {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "caipString cannot be empty")
	}

	// Validate txHash
	if len(msg.TxHash) == 0 {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "txHash cannot be empty")
	}

	return nil
}
