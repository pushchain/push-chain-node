package types

import (
	"encoding/json"
	"fmt"
	"strings"
)

// DefaultParams returns default module parameters.
func DefaultParams() Params {
	return Params{
		Admin: "push1gjaw568e35hjc8udhat0xnsxxmkm2snrexxz20",
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

// ValidateBasic does the sanity check on the params.
func (p Params) ValidateBasic() error {
	if strings.TrimSpace(p.Admin) == "" {
		return fmt.Errorf("admin address cannot be empty")
	}
	return nil
}
