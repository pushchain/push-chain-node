package types

import (
	"encoding/json"
)

// DefaultParams returns default module parameters.
func DefaultParams() Params {
	return Params{
		Admin: "push1negskcfqu09j5zvpk7nhvacnwyy2mafffy7r6a",
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
func (p Params) ValidateBasic() error {
	return nil
}
