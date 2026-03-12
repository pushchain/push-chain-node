package types

import (
	"encoding/json"
)

// Stringer method for ChainMeta.
func (p ChainMeta) String() string {
	bz, err := json.Marshal(p)
	if err != nil {
		panic(err)
	}
	return string(bz)
}

// ValidateBasic does the sanity check on the ChainMeta fields.
func (p ChainMeta) ValidateBasic() error {
	return nil
}
