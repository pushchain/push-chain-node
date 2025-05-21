package types

import (
	"cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

var (
	_ sdk.Msg = &MsgDeployNMSC{}
)

// NewMsgDeployNMSC creates new instance of MsgDeployNMSC
func NewMsgDeployNMSC(
	sender sdk.Address,
	accountId *AccountId,
	txHash string,
) *MsgDeployNMSC {
	return &MsgDeployNMSC{
		Signer:    sender.String(),
		AccountId: accountId,
		TxHash:    txHash,
	}
}

// Route returns the name of the module
func (msg MsgDeployNMSC) Route() string { return ModuleName }

// Type returns the the action
func (msg MsgDeployNMSC) Type() string { return "deploy_nmsc" }

// GetSignBytes implements the LegacyMsg interface.
func (msg MsgDeployNMSC) GetSignBytes() []byte {
	return sdk.MustSortJSON(AminoCdc.MustMarshalJSON(&msg))
}

// GetSigners returns the expected signers for a MsgDeployNMSC message.
func (msg *MsgDeployNMSC) GetSigners() []sdk.AccAddress {
	addr, _ := sdk.AccAddressFromBech32(msg.Signer)
	return []sdk.AccAddress{addr}
}

// ValidateBasic does a sanity check on the provided data.
func (msg *MsgDeployNMSC) ValidateBasic() error {
	// Validate signer
	if _, err := sdk.AccAddressFromBech32(msg.Signer); err != nil {
		return errors.Wrap(err, "invalid signer address")
	}

	// Validate accountId
	if msg.AccountId == nil {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "accountId cannot be nil")
	}

	// Validate txHash
	if len(msg.TxHash) == 0 {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "txHash cannot be empty")
	}

	return msg.AccountId.ValidateBasic()
}
