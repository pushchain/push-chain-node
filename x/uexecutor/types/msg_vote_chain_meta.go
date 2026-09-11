package types

import (
	"cosmossdk.io/errors"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

var (
	_ sdk.Msg = &MsgVoteChainMeta{}
)

// MaxObservedChainIdLen caps the CAIP-2 chain id carried by a chain-meta vote.
//
// F-2026-18803: the id is used verbatim as the ChainMetas map key
// (collections.StringKey), so an uncapped id is an attacker-controlled IAVL key
// of arbitrary size. CAIP-2 itself allows at most 8 (namespace) + 1 + 32
// (reference) = 41 characters, and the longest id we actually register is
// "solana:EtWTRABZaYq6iMfeYKouRu166VU2xqa1" (41). 128 leaves generous headroom
// for future namespaces while keeping the key bounded.
const MaxObservedChainIdLen = 128

// NewMsgVoteChainMeta creates new instance of MsgVoteChainMeta
func NewMsgVoteChainMeta(
	sender sdk.Address,
	observedChainId string,
	price, chainHeight uint64,
) *MsgVoteChainMeta {
	return &MsgVoteChainMeta{
		Signer:          sender.String(),
		ObservedChainId: observedChainId,
		Price:           price,
		ChainHeight:     chainHeight,
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
	if msg.ObservedChainId == "" {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "observed_chain_id cannot be empty")
	}
	// F-2026-18803 (stateless half): ValidateBasic has no keeper, so it cannot
	// ask whether the chain is registered — Keeper.VoteChainMeta does that. What
	// it can do for free at CheckTx time is bound the id's size and shape, so an
	// absurd id is dropped at mempool admission rather than after a block
	// commits it as a ChainMetas key.
	if len(msg.ObservedChainId) > MaxObservedChainIdLen {
		return errors.Wrapf(sdkerrors.ErrInvalidRequest,
			"observed_chain_id exceeds %d characters (got %d)", MaxObservedChainIdLen, len(msg.ObservedChainId))
	}
	if _, _, err := ParseCAIP2(msg.ObservedChainId); err != nil {
		return errors.Wrap(sdkerrors.ErrInvalidRequest,
			"observed_chain_id must be in CAIP-2 format <namespace>:<reference>")
	}
	if msg.Price == 0 {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "price must be greater than 0")
	}
	if msg.ChainHeight == 0 {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "chain_height must be greater than 0")
	}
	return nil
}
