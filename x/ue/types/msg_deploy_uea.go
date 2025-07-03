package types

import (
	"cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

var (
	_ sdk.Msg = &MsgDeployUEA{}
)

// NewMsgDeployUEA creates new instance of MsgDeployUEA
func NewMsgDeployUEA(
	sender sdk.Address,
	universalAccountId *UniversalAccountId,
	txHash string,
) *MsgDeployUEA {
	return &MsgDeployUEA{
		Signer:             sender.String(),
		UniversalAccountId: universalAccountId,
		TxHash:             txHash,
	}
}

// Route returns the name of the module
func (msg MsgDeployUEA) Route() string { return ModuleName }

// Type returns the the action
func (msg MsgDeployUEA) Type() string { return "deploy_uea" }

// GetSignBytes implements the LegacyMsg interface.
func (msg MsgDeployUEA) GetSignBytes() []byte {
	return sdk.MustSortJSON(AminoCdc.MustMarshalJSON(&msg))
}

// GetSigners returns the expected signers for a MsgDeployUEA message.
func (msg *MsgDeployUEA) GetSigners() []sdk.AccAddress {
	addr, _ := sdk.AccAddressFromBech32(msg.Signer)
	return []sdk.AccAddress{addr}
}

// ValidateBasic does a sanity check on the provided data.
func (msg *MsgDeployUEA) ValidateBasic() error {
	// Validate signer
	if _, err := sdk.AccAddressFromBech32(msg.Signer); err != nil {
		return errors.Wrap(err, "invalid signer address")
	}

	// Validate universalAccountId
	if msg.UniversalAccountId == nil {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "universalAccountId cannot be nil")
	}

	// Validate txHash
	if len(msg.TxHash) == 0 {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "txHash cannot be empty")
	}

	return msg.UniversalAccountId.ValidateBasic()
}
