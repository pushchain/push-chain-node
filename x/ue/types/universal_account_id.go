package types

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"cosmossdk.io/errors"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// Stringer method for Params.
func (p UniversalAccountId) String() string {
	bz, err := json.Marshal(p)
	if err != nil {
		panic(err)
	}

	return string(bz)
}

// GetCAIP2 returns the CAIP-2 identifier for the UniversalAccountId.
func (p UniversalAccountId) GetCAIP2() string {
	return fmt.Sprintf("%s:%s", p.ChainNamespace, p.ChainId)
}

// Validate does the sanity check on the params.
func (p UniversalAccountId) ValidateBasic() error {
	p.ChainNamespace = strings.TrimSpace(p.ChainNamespace)
	p.ChainId = strings.TrimSpace(p.ChainId)
	p.Owner = strings.TrimSpace(p.Owner)

	// Validate ChainNamespace and ChainId are non-empty
	if len(p.ChainNamespace) == 0 {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "chain namespace cannot be empty")
	}
	if len(p.ChainId) == 0 {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "chain ID cannot be empty")
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
