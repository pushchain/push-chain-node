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
	p.Chain = strings.TrimSpace(p.Chain)
	p.Owner = strings.TrimSpace(p.Owner)

	// Validate CAIP-2 chain format
	parts := strings.Split(p.Chain, ":")
	if len(p.Chain) == 0 || len(parts) != 2 || len(parts[0]) == 0 || len(parts[1]) == 0 {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "chain must be in CAIP-2 format <namespace>:<reference>")
	}

	// Validate Owner is non-empty
	if len(p.Owner) == 0 {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "owner cannot be empty")
	}

	// Validate owner hex format
	ownerStr := strings.TrimPrefix(p.Owner, "0x")
	_, err := hex.DecodeString(ownerStr)
	if err != nil {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "owner must be valid hex string")
	}

	return nil
}
