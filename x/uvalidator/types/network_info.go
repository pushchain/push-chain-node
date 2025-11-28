package types

import (
	"strings"

	"cosmossdk.io/errors"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// Validate does the sanity check on the network_info params.
func (p NetworkInfo) ValidateBasic() error {
	peerId := strings.TrimSpace(p.PeerId)
	if peerId == "" {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "peerId cannot be empty")
	}

	if p.MultiAddrs == nil {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "multi_addrs cannot be nil")
	}

	if len(p.MultiAddrs) == 0 {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "multi_addrs must contain at least one value")
	}

	return nil
}
