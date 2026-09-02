package types

import (
	"strings"

	"cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

var (
	_ sdk.Msg = &MsgVoteOutbound{}
)

// NewMsgVoteOutbound creates new instance of MsgVoteOutbound
func NewMsgVoteOutbound(
	sender sdk.Address,
	txID, utxID string,
	observedTx OutboundObservation,
) *MsgVoteOutbound {
	return &MsgVoteOutbound{
		Signer:     sender.String(),
		TxId:       txID,
		UtxId:      utxID,
		ObservedTx: &observedTx,
	}
}

// Route returns the name of the module
func (msg MsgVoteOutbound) Route() string { return ModuleName }

// Type returns the action
func (msg MsgVoteOutbound) Type() string { return "msg_vote_outbound" }

// GetSignBytes implements the LegacyMsg interface.
func (msg MsgVoteOutbound) GetSignBytes() []byte {
	return sdk.MustSortJSON(AminoCdc.MustMarshalJSON(&msg))
}

// GetSigners returns the expected signers for a MsgVoteOutbound message.
func (msg *MsgVoteOutbound) GetSigners() []sdk.AccAddress {
	addr, _ := sdk.AccAddressFromBech32(msg.Signer)
	return []sdk.AccAddress{addr}
}

// ValidateBasic does a sanity check on the provided data.
func (msg *MsgVoteOutbound) ValidateBasic() error {
	// validate signer
	if _, err := sdk.AccAddressFromBech32(msg.Signer); err != nil {
		return errors.Wrap(err, "invalid signer address")
	}

	// tx_id must be non-empty
	if strings.TrimSpace(msg.TxId) == "" {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "tx_id cannot be empty")
	}

	// utx_id must be non-empty
	if strings.TrimSpace(msg.UtxId) == "" {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "utx_id cannot be empty")
	}

	// observed_tx must NOT be nil
	if msg.ObservedTx == nil {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "observed_tx cannot be nil")
	}

	// Validate observed_tx content
	return msg.ObservedTx.ValidateBasic()
}
