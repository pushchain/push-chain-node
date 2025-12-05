package types

import (
	"encoding/json"

	"cosmossdk.io/errors"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// Stringer method for Params.
func (p UniversalValidator) String() string {
	bz, err := json.Marshal(p)
	if err != nil {
		panic(err)
	}

	return string(bz)
}

// Validate does the sanity check on the params.
func (p UniversalValidator) ValidateBasic() error {

	// Validate identity info
	if err := p.IdentifyInfo.ValidateBasic(); err != nil {
		return errors.Wrapf(sdkerrors.ErrInvalidRequest, "invalid identify info: %v", err)
	}

	// Validate lifecycle info
	if err := p.LifecycleInfo.ValidateBasic(); err != nil {
		return errors.Wrapf(sdkerrors.ErrInvalidRequest, "invalid lifecycle info: %v", err)
	}

	// Validate network info
	if err := p.NetworkInfo.ValidateBasic(); err != nil {
		return errors.Wrapf(sdkerrors.ErrInvalidRequest, "invalid network info: %v", err)
	}

	return nil
}
