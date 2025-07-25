package types

import (
	"encoding/json"

	"cosmossdk.io/errors"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// Stringer method for BlockConfirmation
func (p BlockConfirmation) String() string {
	bz, err := json.Marshal(p)
	if err != nil {
		panic(err)
	}

	return string(bz)
}

// ValidateBasic performs sanity checks on the BlockConfirmation
func (p BlockConfirmation) ValidateBasic() error {
	if p.FastInbound > p.SlowInbound {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "fast_inbound cannot be greater than slow_inbound confirmations")
	}

	return nil
}
