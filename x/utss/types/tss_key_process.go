package types

import (
	"encoding/json"

	errorsmod "cosmossdk.io/errors"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// Stringer method.
func (p TssKeyProcess) String() string {
	bz, err := json.Marshal(p)
	if err != nil {
		panic(err)
	}

	return string(bz)
}

// ValidateBasic performs basic validation on TssKeyProcess fields
func (p TssKeyProcess) ValidateBasic() error {
	// Validate participants list
	if len(p.Participants) == 0 {
		return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "participants list cannot be empty")
	}
	for i, participant := range p.Participants {
		if participant == "" {
			return errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "participant at index %d is empty", i)
		}
	}

	// Validate block height
	if p.BlockHeight <= 0 {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "invalid block height: %d", p.BlockHeight)
	}

	// Validate expiry height
	if p.ExpiryHeight <= p.BlockHeight {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "expiry height (%d) must be greater than block height (%d)", p.ExpiryHeight, p.BlockHeight)
	}

	// Validate process type
	if p.ProcessType != TssProcessType_TSS_PROCESS_KEYGEN && p.ProcessType != TssProcessType_TSS_PROCESS_REFRESH {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "invalid process type: %v", p.ProcessType)
	}

	// Validate status
	if p.Status != TssKeyProcessStatus_TSS_KEY_PROCESS_PENDING &&
		p.Status != TssKeyProcessStatus_TSS_KEY_PROCESS_SUCCESS &&
		p.Status != TssKeyProcessStatus_TSS_KEY_PROCESS_FAILED {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "invalid process status: %v", p.Status)
	}

	// Validate ID
	if p.Id == 0 {
		return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "process id cannot be zero")
	}

	return nil
}
