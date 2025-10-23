package types

import (
	"cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

var (
	_ sdk.Msg = &MsgVoteInbound{}
)

// NewMsgVoteInbound creates new instance of MsgVoteInbound
func NewMsgVoteInbound(
	sender sdk.Address,
	sourceChain string,
	txHash string,
	recipient string,
	amount string,
	assetAddr string,
	logIndex string,
) *MsgVoteInbound {
	return &MsgVoteInbound{
		Signer: sender.String(),
		Inbound: &Inbound{
			SourceChain: sourceChain,
			TxHash:      txHash,
			Recipient:   recipient,
			Amount:      amount,
			AssetAddr:   assetAddr,
			LogIndex:    logIndex,
		},
	}
}

// Route returns the name of the module
func (msg MsgVoteInbound) Route() string { return ModuleName }

// Type returns the the action
func (msg MsgVoteInbound) Type() string { return "msg_vote_inbound" }

// GetSignBytes implements the LegacyMsg interface.
func (msg MsgVoteInbound) GetSignBytes() []byte {
	return sdk.MustSortJSON(AminoCdc.MustMarshalJSON(&msg))
}

// GetSigners returns the expected signers for a MsgVoteInbound message.
func (msg *MsgVoteInbound) GetSigners() []sdk.AccAddress {
	addr, _ := sdk.AccAddressFromBech32(msg.Signer)
	return []sdk.AccAddress{addr}
}

// ValidateBasic does a sanity check on the provided data.
func (msg *MsgVoteInbound) ValidateBasic() error {
	// validate signer
	if _, err := sdk.AccAddressFromBech32(msg.Signer); err != nil {
		return errors.Wrap(err, "invalid signer address")
	}

	return msg.Inbound.ValidateBasic()
}
