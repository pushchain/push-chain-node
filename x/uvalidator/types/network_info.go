package types

import (
	"strings"

	"cosmossdk.io/errors"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// Validate does the sanity check on the params.
func (p NetworkInfo) ValidateBasic() error {
	// Validate ip is non-empty
	ip := strings.TrimSpace(p.Ip)
	if ip == "" {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "ip cannot be empty")
	}

	return nil
}
