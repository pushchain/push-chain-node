package types

import (
	"encoding/hex"
	"encoding/json"
	"strings"

	"cosmossdk.io/errors"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// Stringer method for Params.
func (p UniversalAccount) String() string {
	bz, err := json.Marshal(p)
	if err != nil {
		panic(err)
	}

	return string(bz)
}

// Validate does the sanity check on the params.
func (p UniversalAccount) ValidateBasic() error {
	// Validate chain is non-empty
	if len(p.Chain) == 0 {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "chain cannot be empty")
	}

	// Validate data (hex string)
	if len(p.Owner) > 0 {
		if _, err := hex.DecodeString(strings.TrimPrefix(p.Owner, "0x")); err != nil {
			return errors.Wrap(sdkerrors.ErrInvalidRequest, "invalid hex data")
		}
	}

	return nil
}
