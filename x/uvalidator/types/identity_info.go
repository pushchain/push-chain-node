package types

import (
	"strings"

	"cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// Validate does the sanity check on the IdentityInfo message type.
func (p IdentityInfo) ValidateBasic() error {
	// Validate core validator address (must be a valid valoper address)
	_, err := sdk.ValAddressFromBech32(p.CoreValidatorAddress)
	if err != nil {
		return errors.Wrap(err, "invalid core validator address")
	}

	// Validate pubkey is non-empty
	pubkey := strings.TrimSpace(p.Pubkey)
	if pubkey == "" {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "pubkey cannot be empty")
	}

	return nil
}
