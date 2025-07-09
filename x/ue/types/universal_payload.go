package types

import (
	"encoding/hex"
	"encoding/json"
	"math/big"
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

// ValidateBasic does the sanity check on the UniversalPayload fields.
func (p UniversalPayload) ValidateBasic() error {
	// Validate 'to' address
	if strings.TrimSpace(p.To) == "" {
		return errors.Wrap(sdkerrors.ErrInvalidAddress, "to address cannot be empty")
	}
	if !utils.IsValidAddress(p.To, utils.HEX) {
		return errors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid to address format: %s", p.To)
	}

	// Validate 'data' is a valid hex string
	if len(p.Data) > 0 {
		if _, err := hex.DecodeString(strings.TrimPrefix(p.Data, "0x")); err != nil {
			return errors.Wrap(sdkerrors.ErrInvalidRequest, "invalid hex data")
		}
	}

	// Validate all numeric string fields as uint256
	uintFields := map[string]string{
		"value":                    p.Value,
		"gas_limit":                p.GasLimit,
		"max_fee_per_gas":          p.MaxFeePerGas,
		"max_priority_fee_per_gas": p.MaxPriorityFeePerGas,
		"nonce":                    p.Nonce,
		"deadline":                 p.Deadline,
	}

	for fieldName, value := range uintFields {
		if value != "" {
			bi, ok := new(big.Int).SetString(value, 10)
			if !ok || bi.Sign() < 0 {
				return errors.Wrapf(sdkerrors.ErrInvalidRequest, "%s must be a valid unsigned integer", fieldName)
			}
		}
	}

	if _, ok := VerificationType_name[int32(p.VType)]; !ok {
		return errors.Wrapf(sdkerrors.ErrInvalidRequest, "invalid verificationData type: %v", p.VType)
	}

	return nil
}
