package precompile_util

import (
	"fmt"

	cmn "github.com/cosmos/evm/precompiles/common"
	"github.com/ethereum/go-ethereum/common"
)

func ParseAddressFrom(args []interface{}, argIndex int) (common.Address, error) {
	if len(args) < 1 {
		return common.Address{}, fmt.Errorf(cmn.ErrInvalidNumberOfArgs, 1, len(args))
	}

	address, ok := args[argIndex].(common.Address)
	if !ok {
		return common.Address{}, fmt.Errorf(cmn.ErrInvalidType, "erc20Address", common.Address{}, args[0])
	}

	return address, nil
}

func ParseStringFrom(args []interface{}, argIndex int) (string, error) {
	if len(args) < 1 {
		return "", fmt.Errorf(cmn.ErrInvalidNumberOfArgs, 1, len(args))
	}

	value, ok := args[argIndex].(string)
	if !ok {
		return "", fmt.Errorf(cmn.ErrInvalidType, "string", "", args[0])
	}

	return value, nil
}

// ConvertToStringArray converts an interface value to a string array.
// It handles both direct []string types and []interface{} that contains strings.
// argPosition is used in error messages to identify which argument had the issue.
func ConvertToStringArray(arg interface{}, argPosition int) ([]string, error) {
	// Check if it's already a string array
	addressesInterface, ok := arg.([]string)
	if ok {
		return addressesInterface, nil
	}

	// Try to convert from interface{} array to string array if needed
	addressesArrayInterface, ok := arg.([]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid addresses format at position %d: expected string array", argPosition)
	}

	// Convert from []interface{} to []string
	addressesInterface = make([]string, len(addressesArrayInterface))
	for i, addr := range addressesArrayInterface {
		addrStr, ok := addr.(string)
		if !ok {
			return nil, fmt.Errorf("invalid address at index %d in argument position %d: expected string", i, argPosition)
		}
		addressesInterface[i] = addrStr
	}

	return addressesInterface, nil
}
