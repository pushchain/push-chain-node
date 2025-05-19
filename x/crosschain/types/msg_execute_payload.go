package types

import (
	"encoding/hex"
	"strings"

	"cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/rollchains/pchain/util"
)

var (
	_ sdk.Msg = &MsgExecutePayload{}
)

// NewMsgExecutePayload creates new instance of MsgExecutePayload
func NewMsgExecutePayload(
	sender sdk.Address,
	caipString string,
	crosschain_payload *CrossChainPayload,
	signature string,
) *MsgExecutePayload {
	return &MsgExecutePayload{
		Signer:            sender.String(),
		CaipString:        caipString,
		CrosschainPayload: crosschain_payload,
		Signature:         signature,
	}
}

// Route returns the name of the module
func (msg MsgExecutePayload) Route() string { return ModuleName }

// Type returns the the action
func (msg MsgExecutePayload) Type() string { return "deploy_nmsc" }

// GetSignBytes implements the LegacyMsg interface.
func (msg MsgExecutePayload) GetSignBytes() []byte {
	return sdk.MustSortJSON(AminoCdc.MustMarshalJSON(&msg))
}

// GetSigners returns the expected signers for a MsgExecutePayload message.
func (msg *MsgExecutePayload) GetSigners() []sdk.AccAddress {
	addr, _ := sdk.AccAddressFromBech32(msg.Signer)
	return []sdk.AccAddress{addr}
}

// ValidateBasic does a sanity check on the provided data.
func (msg *MsgExecutePayload) Validate() error {
	// Validate signer
	if _, err := sdk.AccAddressFromBech32(msg.Signer); err != nil {
		return errors.Wrap(err, "invalid signer address")
	}

	// Validate caipString
	if len(msg.CaipString) == 0 {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "caipString cannot be empty")
	}

	// Validate crosschain payload
	if msg.CrosschainPayload == nil {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "crosschain payload cannot be nil")
	}

	// Validate target address
	isValidTarget := util.IsValidAddress(msg.CrosschainPayload.Target, util.HEX)
	if !isValidTarget {
		return errors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid target address format: %s", msg.CrosschainPayload.Target)
	}

	// Validate data (hex string)
	if len(msg.CrosschainPayload.Data) > 0 {
		if _, err := hex.DecodeString(strings.TrimPrefix(msg.CrosschainPayload.Data, "0x")); err != nil {
			return errors.Wrap(sdkerrors.ErrInvalidRequest, "invalid hex data")
		}
	}

	// Validate signature
	if len(msg.Signature) == 0 {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "signature cannot be empty")
	}
	if _, err := hex.DecodeString(strings.TrimPrefix(msg.Signature, "0x")); err != nil {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "invalid signature hex")
	}

	return nil
}
