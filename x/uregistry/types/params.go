package types

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Default Admin needs to be added explicityly in genesis file
func DefaultParams() Params {
	return Params{
		Admin: "",
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
	if strings.TrimSpace(p.Admin) == "" {
		return fmt.Errorf("admin address cannot be empty")
	}
	return nil
}
