package types

import (
	"encoding/json"
)

// Stringer method for Params.
func (p AdminParams) String() string {
	bz, err := json.Marshal(p)
	if err != nil {
		panic(err)
	}

	return string(bz)
}

// Validate does the sanity check on the params.
func (p AdminParams) Validate() error {
	// TODO:
	return nil
}
