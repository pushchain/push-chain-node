package types

import (
	"encoding/json"
)

// Stringer method for Params.
func (p GasPrice) String() string {
	bz, err := json.Marshal(p)
	if err != nil {
		panic(err)
	}

	return string(bz)
}

// ValidateBasic does the sanity check on the GasPrice fields.
func (p GasPrice) ValidateBasic() error {
	return nil
}
