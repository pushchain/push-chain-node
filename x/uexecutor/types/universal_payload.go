package types

import (
	"encoding/hex"
	"encoding/json"
	"strings"

	"cosmossdk.io/errors"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/pushchain/push-chain-node/utils"
)

// Stringer method for Params.
func (p UniversalPayload) String() string {
	bz, err := json.Marshal(p)
	if err != nil {
		panic(err)
	}

	return string(bz)
}

// ValidateSize enforces the flat MaxUniversalPayloadBytes cap on the serialized
// payload. Split out of ValidateBasic so the keeper can apply the cap on its
// own, independently of whichever msg carried the payload in.
func (p *UniversalPayload) ValidateSize() error {
	if p == nil {
		return nil
	}
	if n := p.Size(); n > MaxUniversalPayloadBytes {
		return errors.Wrapf(sdkerrors.ErrInvalidRequest,
			"universal payload too large: %d bytes exceeds the %d byte limit", n, MaxUniversalPayloadBytes)
	}
	return nil
}

// ValidateOutboundPayloadBlobSize enforces MaxOutboundPayloadBytes on an
// outbound's payload.
func ValidateOutboundPayloadBlobSize(field, blob string) error {
	if len(blob) > MaxOutboundPayloadBytes {
		return errors.Wrapf(sdkerrors.ErrInvalidRequest,
			"%s too large: %d bytes exceeds the %d byte limit", field, len(blob), MaxOutboundPayloadBytes)
	}
	return nil
}

// ValidatePayloadBlobSize enforces MaxUniversalPayloadBytes on a hex blob that
// carries a universal payload (or its verification data) before it is decoded.
func ValidatePayloadBlobSize(field, blob string) error {
	if len(blob) > MaxUniversalPayloadBytes {
		return errors.Wrapf(sdkerrors.ErrInvalidRequest,
			"%s too large: %d bytes exceeds the %d byte limit", field, len(blob), MaxUniversalPayloadBytes)
	}
	return nil
}

// ValidateBasic does the sanity check on the UniversalPayload fields.
func (p UniversalPayload) ValidateBasic() error {
	// Reject oversized payloads before any of the per-field work below.
	if err := p.ValidateSize(); err != nil {
		return err
	}

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
			// Length-capped, range-checked uint256 parse — see F-2026-18798.
			if _, err := ValidateUint256String(value, fieldName+" must be a valid unsigned integer"); err != nil {
				return err
			}
		}
	}

	if _, ok := VerificationType_name[int32(p.VType)]; !ok {
		return errors.Wrapf(sdkerrors.ErrInvalidRequest, "invalid verificationData type: %v", p.VType)
	}

	return nil
}
