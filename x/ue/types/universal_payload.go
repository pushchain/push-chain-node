package types

import (
	"encoding/hex"
	"encoding/json"
	"strings"

	"cosmossdk.io/errors"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/rollchains/pchain/utils"
)

// Stringer method for Params.
func (p UniversalPayload) String() string {
	bz, err := json.Marshal(p)
	if err != nil {
		panic(err)
	}

	return string(bz)
}

// Validate does the sanity check on the params.
func (p UniversalPayload) ValidateBasic() error {
	// Validate to address
	isValidTo := utils.IsValidAddress(p.To, utils.HEX)
	if !isValidTo {
		return errors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid to address format: %s", p.To)
	}

	// Validate data (hex string)
	if len(p.Data) > 0 {
		if _, err := hex.DecodeString(strings.TrimPrefix(p.Data, "0x")); err != nil {
			return errors.Wrap(sdkerrors.ErrInvalidRequest, "invalid hex data")
		}
	}

	return nil
}
