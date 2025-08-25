package types

import (
	"encoding/json"
)

// Stringer method for Params.
func (p OutboundTx) String() string {
	bz, err := json.Marshal(p)
	if err != nil {
		panic(err)
	}

	return string(bz)
}

// TODO: update the validation fn
// Validate does the sanity check on the params.
func (p OutboundTx) ValidateBasic() error {
	// Validate chain is non-empty and follows CAIP-2 format
	return nil
}
