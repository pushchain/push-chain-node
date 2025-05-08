package util

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

// get address pair returns both the cosmos and the 0x addresses, or an error
func GetAddressPair(addr string) (sdk.AccAddress, common.Address, error) {
	bz, err := ConvertAnyAddressToBytes(addr)
	if err != nil {
		return nil, common.Address{}, err
	}

	return sdk.AccAddress(bz), common.BytesToAddress(bz), nil
}

func MustConvertCosmosToHex(addr string) string {
	bz, err := ConvertAnyAddressToBytes(addr)
	if err != nil {
		return ""
	}
	return common.Address(bz).Hex()
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
