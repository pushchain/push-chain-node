package types

import (
	"encoding/json"
)

// Stringer method for Params.
func (s SystemConfig) String() string {
	bz, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}

	return string(bz)
}

// Validate does the sanity check on the system config params.
func (s SystemConfig) ValidateBasic() error {
	return nil
}
