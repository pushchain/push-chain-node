package types

import (
	"encoding/json"
	"strings"

	"cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// Stringer method for Params.
func (p UniversalValidator) String() string {
	bz, err := json.Marshal(p)
	if err != nil {
		panic(err)
	}

	return string(bz)
}

// Validate does the sanity check on the params.
func (p UniversalValidator) ValidateBasic() error {
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

	// Validate uv_status is within known enum range
	if _, ok := UVStatus_name[int32(p.Status)]; !ok {
		return errors.Wrapf(sdkerrors.ErrInvalidRequest, "invalid uv_status: %v", p.Status)
	}

	return p.Network.ValidateBasic()
}
