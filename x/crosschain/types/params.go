package types

import (
	"encoding/json"

	"cosmossdk.io/errors"
	sdkErrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/rollchains/pchain/util"
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
func (p Params) Validate() error {
	isValidAdmin := util.IsValidAddress(p.Admin, util.COSMOS)
	if !isValidAdmin {
		return errors.Wrapf(sdkErrors.ErrInvalidAddress, "invalid admin address: %s", p.Admin)
	}
	return nil
}
