package types

import (
	"cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// Validate does the sanity check on the IdentityInfo message type.
func (p IdentityInfo) ValidateBasic() error {
	// Validate core validator address (must be a valid valoper address)
	_, err := sdk.ValAddressFromBech32(p.CoreValidatorAddress)
	if err != nil {
		return errors.Wrap(err, "invalid core validator address")
	}

	return nil
}
