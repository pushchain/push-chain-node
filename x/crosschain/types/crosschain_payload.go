package types

import (
	"encoding/hex"
	"encoding/json"
	"strings"

	"cosmossdk.io/errors"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/rollchains/pchain/util"
)

// Stringer method for Params.
func (p CrossChainPayload) String() string {
	bz, err := json.Marshal(p)
	if err != nil {
		panic(err)
	}

	return string(bz)
}

// Validate does the sanity check on the params.
func (p CrossChainPayload) Validate() error {
	// Validate target address
	isValidTarget := util.IsValidAddress(p.Target, util.HEX)
	if !isValidTarget {
		return errors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid target address format: %s", p.Target)
	}

	// Validate data (hex string)
	if len(p.Data) > 0 {
		if _, err := hex.DecodeString(strings.TrimPrefix(p.Data, "0x")); err != nil {
			return errors.Wrap(sdkerrors.ErrInvalidRequest, "invalid hex data")
		}
	}

	return nil
}
