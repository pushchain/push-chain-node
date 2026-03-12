package types

import (
	"cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

var (
	_ sdk.Msg = &MsgVoteChainMeta{}
)

// NewMsgVoteChainMeta creates new instance of MsgVoteChainMeta
func NewMsgVoteChainMeta(
	sender sdk.Address,
	observedChainId string,
	price, chainHeight, observedAt uint64,
) *MsgVoteChainMeta {
	return &MsgVoteChainMeta{
		Signer:          sender.String(),
		ObservedChainId: observedChainId,
		Price:           price,
		ChainHeight:     chainHeight,
		ObservedAt:      observedAt,
	}
}

// Route returns the name of the module
func (msg MsgVoteChainMeta) Route() string { return ModuleName }

// Type returns the action
func (msg MsgVoteChainMeta) Type() string { return "msg_vote_chain_meta" }

// GetSignBytes implements the LegacyMsg interface.
func (msg MsgVoteChainMeta) GetSignBytes() []byte {
	return sdk.MustSortJSON(AminoCdc.MustMarshalJSON(&msg))
}

// GetSigners returns the expected signers for a MsgVoteChainMeta message.
func (msg *MsgVoteChainMeta) GetSigners() []sdk.AccAddress {
	addr, _ := sdk.AccAddressFromBech32(msg.Signer)
	return []sdk.AccAddress{addr}
}

// ValidateBasic does a sanity check on the provided data.
func (msg *MsgVoteChainMeta) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(msg.Signer); err != nil {
		return errors.Wrap(err, "invalid signer address")
	}
	return nil
}
