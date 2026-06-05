package types

import (
	"encoding/json"
	"strings"

	"cosmossdk.io/errors"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"

	"github.com/pushchain/push-chain-node/utils"
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

	// PRC20s live on Push Chain (EVM): must be a parseable 20-byte hex address
	// so the PRC20 reverse index always carries the canonical EIP-55 form.
	if p.ContractAddress != "" {
		if _, err := utils.CanonicalizeEVMAddress(p.ContractAddress); err != nil {
			return errors.Wrapf(sdkerrors.ErrInvalidRequest, "invalid contract_address: %s", err)
		}
	}

	return nil
}
