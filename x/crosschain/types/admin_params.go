package types

import (
	"encoding/json"

	"cosmossdk.io/errors"
	sdkErrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/push-protocol/push-chain/util"
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
	isValidFactoryAddr := util.IsValidAddress(p.FactoryAddress, util.HEX)
	if !isValidFactoryAddr {
		return errors.Wrapf(sdkErrors.ErrInvalidAddress, "invalid factory address: %s", p.FactoryAddress)
	}

	return nil
}
