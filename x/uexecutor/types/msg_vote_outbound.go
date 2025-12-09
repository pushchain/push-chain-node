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
	txID string,
	observedTx OutboundObservation,
) *MsgVoteOutbound {
	return &MsgVoteOutbound{
		Signer:     sender.String(),
		TxId:       txID,
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

	// Decode tx_id into (utxID, outboundID)
	utxID, outboundID, err := DecodeOutboundTxIDHex(msg.TxId)
	if err != nil {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "invalid tx_id: decode failed")
	}

	if strings.TrimSpace(utxID) == "" {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "decoded utx_id cannot be empty")
	}
	if strings.TrimSpace(outboundID) == "" {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "decoded outbound_id cannot be empty")
	}

	// observed_tx must NOT be nil
	if msg.ObservedTx == nil {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "observed_tx cannot be nil")
	}

	// Validate observed_tx content
	obs := msg.ObservedTx

	if obs.Success {
		// Success requires tx_hash AND block_height > 0
		if strings.TrimSpace(obs.TxHash) == "" {
			return errors.Wrap(sdkerrors.ErrInvalidRequest,
				"observed_tx.tx_hash required when success=true")
		}
		if obs.BlockHeight == 0 {
			return errors.Wrap(sdkerrors.ErrInvalidRequest,
				"observed_tx.block_height must be > 0 when success=true")
		}

	} else {
		// Failure case:
		// tx_hash MAY be empty.
		// BUT if tx_hash is present, block_height must be > 0.
		if strings.TrimSpace(obs.TxHash) != "" && obs.BlockHeight == 0 {
			return errors.Wrap(sdkerrors.ErrInvalidRequest,
				"observed_tx.block_height must be > 0 when tx_hash is provided")
		}
	}

	return nil
}
