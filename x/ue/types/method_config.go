package types

import (
	"encoding/json"

	"cosmossdk.io/errors"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// Stringer method for Params.
func (p MethodConfig) String() string {
	bz, err := json.Marshal(p)
	if err != nil {
		panic(err)
	}

	return string(bz)
}

// Validate does the sanity check on the params.
func (p MethodConfig) ValidateBasic() error {

	// Validate method name, selector, and event topic are non-empty
	if len(p.Name) == 0 {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "method name cannot be empty")
	}
	if len(p.Selector) == 0 {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "method selector cannot be empty")
	}
	if len(p.EventTopic) == 0 {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "method event_topic cannot be empty")
	}

	return nil
}
