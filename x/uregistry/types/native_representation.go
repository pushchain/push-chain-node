package types

import (
	"encoding/json"
	"strings"

	"cosmossdk.io/errors"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// Stringer method for NativeRepresentation
func (p NativeRepresentation) String() string {
	bz, err := json.Marshal(p)
	if err != nil {
		panic(err)
	}

	return string(bz)
}

// ValidateBasic performs sanity checks on the NativeRepresentation
func (p NativeRepresentation) ValidateBasic() error {
	// If both fields are empty, that's allowed (optional native_representation)
	if strings.TrimSpace(p.Denom) == "" && strings.TrimSpace(p.ContractAddress) == "" {
		return nil
	}

	// If contract address is set, it must be a 0x-prefixed valid format (basic check)
	if p.ContractAddress != "" && !strings.HasPrefix(p.ContractAddress, "0x") {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "contract_address must start with 0x")
	}

	return nil
}
