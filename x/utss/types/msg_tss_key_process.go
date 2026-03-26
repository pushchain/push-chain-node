package types

import (
	"strings"

	"cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

var (
	_ sdk.Msg = &MsgInitiateTssKeyProcess{}
	_ sdk.Msg = &MsgVoteTssKeyProcess{}
)

// ValidateBasic does a sanity check on the provided data.
func (msg *MsgInitiateTssKeyProcess) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(msg.Signer); err != nil {
		return errors.Wrap(sdkerrors.ErrInvalidAddress, "invalid signer address")
	}

	// ProcessType is an enum — 0 (KEYGEN), 1 (REFRESH), 2 (QUORUM_CHANGE) are all valid.
	// No further validation needed since protobuf enforces the enum range.

	return nil
}

// ValidateBasic does a sanity check on the provided data.
func (msg *MsgVoteTssKeyProcess) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(msg.Signer); err != nil {
		return errors.Wrap(sdkerrors.ErrInvalidAddress, "invalid signer address")
	}

	if strings.TrimSpace(msg.TssPubkey) == "" {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "tss_pubkey cannot be empty")
	}

	if strings.TrimSpace(msg.KeyId) == "" {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "key_id cannot be empty")
	}

	if msg.ProcessId == 0 {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "process_id must be greater than 0")
	}

	return nil
}
