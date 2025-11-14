package types

import (
	"encoding/json"
	"math/big"
	"strings"

	"cosmossdk.io/errors"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/pushchain/push-chain-node/utils"
)

// Stringer method for Params.
func (p MigrationPayload) String() string {
	bz, err := json.Marshal(p)
	if err != nil {
		panic(err)
	}

	return string(bz)
}

// ValidateBasic does the sanity check on the UniversalPayload fields.
func (p MigrationPayload) ValidateBasic() error {
	// Validate 'to' address
	if strings.TrimSpace(p.Migration) == "" {
		return errors.Wrap(sdkerrors.ErrInvalidAddress, "to address cannot be empty")
	}
	if !utils.IsValidAddress(p.Migration, utils.HEX) {
		return errors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid to address format: %s", p.Migration)
	}

	// Validate all numeric string fields as uint256
	uintFields := map[string]string{
		"nonce":    p.Nonce,
		"deadline": p.Deadline,
	}

	for fieldName, value := range uintFields {
		if value != "" {
			bi, ok := new(big.Int).SetString(value, 10)
			if !ok || bi.Sign() < 0 {
				return errors.Wrapf(sdkerrors.ErrInvalidRequest, "%s must be a valid unsigned integer", fieldName)
			}
		}
	}

	return nil
}
