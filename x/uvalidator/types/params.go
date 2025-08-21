package types

import (
	"encoding/json"
)

// DefaultParams returns default module parameters.
func DefaultParams() Params {
	return Params{
		Admin: "push1gjaw568e35hjc8udhat0xnsxxmkm2snrexxz20", // added acc1 as default admin for now
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
