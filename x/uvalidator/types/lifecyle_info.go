package types

import (
	"cosmossdk.io/errors"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// ValidateBasic performs stateless validation on the LifecycleInfo struct.
// Ensures that current status is valid and lifecycle history entries are consistent.
func (p LifecycleInfo) ValidateBasic() error {
	// Validate that the current_status is within known enum range
	if _, ok := UVStatus_name[int32(p.CurrentStatus)]; !ok {
		return errors.Wrapf(sdkerrors.ErrInvalidRequest, "invalid current_status: %v", p.CurrentStatus)
	}

	// Validate each lifecycle event in history
	for i, event := range p.History {
		if err := event.ValidateBasic(); err != nil {
			return errors.Wrapf(sdkerrors.ErrInvalidRequest, "invalid history[%d]: %v", i, err)
		}
	}

	// Ensure the lifecycle history is ordered by block height
	for i := 1; i < len(p.History); i++ {
		if p.History[i].BlockHeight < p.History[i-1].BlockHeight {
			return errors.Wrapf(
				sdkerrors.ErrInvalidRequest,
				"history not ordered: event[%d] (height %d) < event[%d] (height %d)",
				i, p.History[i].BlockHeight, i-1, p.History[i-1].BlockHeight,
			)
		}
	}

	return nil
}
