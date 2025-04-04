package types

import (
	"encoding/json"

	"cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// DefaultParams returns default module parameters.
func DefaultParams() Params {
	// TODO:
	return Params{
		SomeValue: true,
	}
}

// Stringer method for Params.
func (p Params) String() string {
	bz, err := json.Marshal(p)
	if err != nil {
		panic(err)
	}

	return string(bz)
}

// Validate does the sanity check on the params.
func (p Params) Validate() error {
	// TODO:
	if p.AdminAddress != "" {
		if _, err := sdk.AccAddressFromBech32(p.AdminAddress); err != nil {
			return errors.Wrap(err, "invalid admin address")
		}
	}

	return nil
}
