package types

import (
	"cosmossdk.io/errors"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// Validate does the sanity check on the LifecycleEvent message type.
func (p LifecycleEvent) ValidateBasic() error {
	// Validate uv_status is within known enum range
	if _, ok := UVStatus_name[int32(p.Status)]; !ok {
		return errors.Wrapf(sdkerrors.ErrInvalidRequest, "invalid uv_status: %v", p.Status)
	}

	return nil
}
