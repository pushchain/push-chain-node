package types

import (
	"cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/pushchain/push-chain-node/utils"
)

var (
	_ sdk.Msg = &MsgUpdateParams{}
)

// NewMsgUpdateParams creates new instance of MsgUpdateParams
func NewMsgUpdateParams(
	sender sdk.Address,
	admin sdk.Address,
) *MsgUpdateParams {
	return &MsgUpdateParams{
		Authority: sender.String(),
		Params: Params{
			Admin: admin.String(),
		},
	}
}

// Route returns the name of the module
func (msg MsgUpdateParams) Route() string { return ModuleName }

// Type returns the the action
func (msg MsgUpdateParams) Type() string { return "update_params" }

// GetSignBytes implements the LegacyMsg interface.
func (msg MsgUpdateParams) GetSignBytes() []byte {
	return sdk.MustSortJSON(AminoCdc.MustMarshalJSON(&msg))
}

// GetSigners returns the expected signers for a MsgUpdateParams message.
func (msg *MsgUpdateParams) GetSigners() []sdk.AccAddress {
	addr, _ := sdk.AccAddressFromBech32(msg.Authority)
	return []sdk.AccAddress{addr}
}

// ValidateBasic does a sanity check on the provided data.
func (msg *MsgUpdateParams) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(msg.Authority); err != nil {
		return errors.Wrap(err, "invalid authority address")
	}

	isValidAdmin := utils.IsValidAddress(msg.Params.Admin, utils.COSMOS)
	if !isValidAdmin {
		return errors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid admin address: %s", msg.Params.Admin)
	}

	return msg.Params.ValidateBasic()
}
