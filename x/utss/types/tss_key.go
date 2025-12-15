package types

import (
	"encoding/json"
	"strings"

	errorsmod "cosmossdk.io/errors"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// String returns a JSON string representation of TssKey
func (p TssKey) String() string {
	bz, err := json.Marshal(p)
	if err != nil {
		panic(err)
	}
	return string(bz)
}

// ValidateBasic performs basic validation on TssKey fields
func (p TssKey) ValidateBasic() error {
	// Validate TSS public key
	if strings.TrimSpace(p.TssPubkey) == "" {
		return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "tss_pubkey cannot be empty")
	}

	// Validate Key ID
	if strings.TrimSpace(p.KeyId) == "" {
		return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "key_id cannot be empty")
	}

	// Validate participants
	if len(p.Participants) == 0 {
		return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "participants list cannot be empty")
	}
	for i, participant := range p.Participants {
		if strings.TrimSpace(participant) == "" {
			return errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "participant at index %d is empty", i)
		}
	}

	// Validate keygen and finalized block heights
	if p.KeygenBlockHeight <= 0 {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "invalid keygen_block_height: %d", p.KeygenBlockHeight)
	}
	if p.FinalizedBlockHeight < p.KeygenBlockHeight {
		return errorsmod.Wrapf(
			sdkerrors.ErrInvalidRequest,
			"finalized_block_height (%d) cannot be less than keygen_block_height (%d)",
			p.FinalizedBlockHeight, p.KeygenBlockHeight,
		)
	}

	// Validate process ID
	if p.ProcessId == 0 {
		return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "process_id cannot be zero")
	}

	return nil
}
