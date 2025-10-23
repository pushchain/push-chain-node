package types

import (
	"cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

var (
	_ sdk.Msg = &MsgVoteGasPrice{}
)

// NewMsgVoteGasPrice creates new instance of MsgVoteGasPrice
func NewMsgVoteGasPrice(
	sender sdk.Address,
	chainId string,
	price, blockNum uint64,
) *MsgVoteGasPrice {
	return &MsgVoteGasPrice{
		Signer:      sender.String(),
		ChainId:     chainId,
		Price:       price,
		BlockNumber: blockNum,
	}
}

// Route returns the name of the module
func (msg MsgVoteGasPrice) Route() string { return ModuleName }

// Type returns the the action
func (msg MsgVoteGasPrice) Type() string { return "msg_vote_gas_price" }

// GetSignBytes implements the LegacyMsg interface.
func (msg MsgVoteGasPrice) GetSignBytes() []byte {
	return sdk.MustSortJSON(AminoCdc.MustMarshalJSON(&msg))
}

// GetSigners returns the expected signers for a MsgVoteGasPrice message.
func (msg *MsgVoteGasPrice) GetSigners() []sdk.AccAddress {
	addr, _ := sdk.AccAddressFromBech32(msg.Signer)
	return []sdk.AccAddress{addr}
}

// ValidateBasic does a sanity check on the provided data.
func (msg *MsgVoteGasPrice) ValidateBasic() error {
	// validate signer
	if _, err := sdk.AccAddressFromBech32(msg.Signer); err != nil {
		return errors.Wrap(err, "invalid signer address")
	}

	return nil
}
