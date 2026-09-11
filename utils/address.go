package utils

import (
	"fmt"

	"github.com/ethereum/go-ethereum/common"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// convert the 0x and/or cosmos address to raw bytes
func ConvertAnyAddressToBytes(addr string) ([]byte, error) {
	if len(addr) == 0 {
		return common.Address{}.Bytes(), nil
	}

	if common.IsHexAddress(addr) {
		return common.FromHex(addr), nil
	}

	return sdk.AccAddressFromBech32(addr)
}

// Type constraint that allows either []byte or common.Address
type ByteType interface {
	[]byte | common.Address
}

// return either []byte or common.Address
func ConvertAnyAddressesToBytes[T ByteType](addr ...string) ([]T, error) {
	var res []T
	for _, a := range addr {
		// Get the raw bytes first
		rawBytes, err := ConvertAnyAddressToBytes(a)
		if err != nil {
			return nil, err
		}

		// Convert to the appropriate type
		var converted T
		switch any(converted).(type) {
		case []byte:
			// If T is []byte, just use the bytes directly
			converted = any(rawBytes).(T)
		case common.Address:
			// If T is common.Address, convert bytes to Address
			var address common.Address
			// Make sure we have the right number of bytes
			if len(rawBytes) != common.AddressLength {
				return nil, fmt.Errorf("invalid address length: got %d, want %d", len(rawBytes), common.AddressLength)
			}
			copy(address[:], rawBytes)
			converted = any(address).(T)
		}

		res = append(res, converted)
	}
	return res, nil
}

// GetAddressPair returns both the cosmos and the 0x addresses, or an error.
//
// The address MUST decode to exactly 20 bytes. The Cosmos SDK accepts bech32
// account addresses of up to 255 bytes, while common.BytesToAddress silently
// keeps only the RIGHTMOST 20 bytes. A longer address would therefore collapse
// onto an unrelated EVM address - e.g. 0x01 || <uexecutor module address>
// truncates to the uexecutor module itself - so reject it instead of
// truncating.
func GetAddressPair(addr string) (sdk.AccAddress, common.Address, error) {
	bz, err := ConvertAnyAddressToBytes(addr)
	if err != nil {
		return nil, common.Address{}, err
	}

	if len(bz) != common.AddressLength {
		return nil, common.Address{}, fmt.Errorf(
			"invalid address length for %q: got %d bytes, want %d", addr, len(bz), common.AddressLength)
	}

	return sdk.AccAddress(bz), common.BytesToAddress(bz), nil
}

// MustConvertCosmosToHex returns the 0x form of addr, or an empty string when
// addr cannot be represented as a 20-byte EVM address.
//
// It never panics and never truncates: the previous common.Address(bz)
// conversion panicked for inputs shorter than 20 bytes and silently kept the
// LEFTMOST 20 bytes for longer ones - the opposite end from
// common.BytesToAddress used elsewhere in this file.
func MustConvertCosmosToHex(addr string) string {
	bz, err := ConvertAnyAddressToBytes(addr)
	if err != nil || len(bz) != common.AddressLength {
		return ""
	}
	return common.BytesToAddress(bz).Hex()
}

// create an enum for COSMOS, 0x, or EITHER
type AddressType int

const (
	COSMOS AddressType = iota
	HEX
	EITHER
)

// IsValidAddress checks if the address is a valid COSMOS, HEX (0x), or EITHER address
func IsValidAddress(addr string, at AddressType) bool {
	switch at {
	case COSMOS:
		_, err := sdk.AccAddressFromBech32(addr)
		return err == nil
	case HEX:
		return common.IsHexAddress(addr)
	case EITHER:
		_, err := ConvertAnyAddressToBytes(addr)
		return err == nil
	default:
		return false
	}
}
