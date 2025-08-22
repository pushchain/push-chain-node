package types

import (
	"encoding/hex"
	"strings"

	"cosmossdk.io/errors"
	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

var (
	_ sdk.Msg = &MsgVoteInboundSynthetic{}
)

// NewMsgVoteInboundSynthetic creates new instance of MsgVoteInboundSynthetic
func NewMsgVoteInboundSynthetic(
	sender sdk.Address,
	sourceChain string,
	txHash string,
	recipient string,
	amount string,
	assetAddr string,
	logIndex string,
) *MsgVoteInboundSynthetic {
	return &MsgVoteInboundSynthetic{
		Signer: sender.String(),
		InboundSynthetic: &InboundSynthetic{
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
func (msg MsgVoteInboundSynthetic) Route() string { return ModuleName }

// Type returns the the action
func (msg MsgVoteInboundSynthetic) Type() string { return "mint_pc" }

// GetSignBytes implements the LegacyMsg interface.
func (msg MsgVoteInboundSynthetic) GetSignBytes() []byte {
	return sdk.MustSortJSON(AminoCdc.MustMarshalJSON(&msg))
}

// GetSigners returns the expected signers for a MsgVoteInboundSynthetic message.
func (msg *MsgVoteInboundSynthetic) GetSigners() []sdk.AccAddress {
	addr, _ := sdk.AccAddressFromBech32(msg.Signer)
	return []sdk.AccAddress{addr}
}

// ValidateBasic does a sanity check on the provided data.
// ValidateBasic does a sanity check on the provided data.
func (msg *MsgVoteInboundSynthetic) ValidateBasic() error {
	// validate signer
	if _, err := sdk.AccAddressFromBech32(msg.Signer); err != nil {
		return errors.Wrap(err, "invalid signer address")
	}

	// source chain (e.g. eip155:11155111)
	if strings.TrimSpace(msg.InboundSynthetic.SourceChain) == "" {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "source chain cannot be empty")
	}

	// tx hash (from source chain)
	if strings.TrimSpace(msg.InboundSynthetic.TxHash) == "" {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "tx hash cannot be empty")
	}

	// Validate recipient is non-empty
	if len(msg.InboundSynthetic.Recipient) == 0 {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "recipient cannot be empty")
	}

	// Validate recipient hex format
	recipientStr := strings.TrimPrefix(msg.InboundSynthetic.Recipient, "0x")
	_, err := hex.DecodeString(recipientStr)
	if err != nil {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "recipient must be valid hex string")
	}

	// validate amount: must be positive integer string
	amt, ok := sdkmath.NewIntFromString(msg.InboundSynthetic.Amount)
	if !ok || !amt.IsPositive() {
		return errors.Wrap(sdkerrors.ErrInvalidCoins, "amount must be a positive integer")
	}

	// validate asset address
	if strings.TrimSpace(msg.InboundSynthetic.AssetAddr) == "" {
		return errors.Wrap(sdkerrors.ErrInvalidAddress, "asset address cannot be empty")
	}

	// validate asset address
	if strings.TrimSpace(msg.InboundSynthetic.LogIndex) == "" {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "log index cannot be empty")
	}

	return nil
}
