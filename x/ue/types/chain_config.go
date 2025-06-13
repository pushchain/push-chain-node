package types

import (
	"encoding/json"

	"cosmossdk.io/errors"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// Stringer method for Params.
func (p ChainConfig) String() string {
	bz, err := json.Marshal(p)
	if err != nil {
		panic(err)
	}

	return string(bz)
}

// Validate does the sanity check on the params.
func (p ChainConfig) ValidateBasic() error {

	// Validate chain is non-empty
	if len(p.Chain) == 0 {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "chain cannot be empty")
	}

	// Validate publicRpcUrl is non-empty
	if len(p.PublicRpcUrl) == 0 {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "public_rpc_url cannot be empty")
	}

	// Validate lockerContractAddress is non-empty
	if len(p.LockerContractAddress) == 0 {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "locker_contract_address cannot be empty")
	}

	// Ensure vm_type is within the known enum range
	if p.VmType < 0 || int(p.VmType) > int(VM_TYPE_OTHER_VM) {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "invalid vm_type")
	}

	// Validate gateway_methods is not empty
	if len(p.GatewayMethods) == 0 {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "gateway_methods cannot be empty")
	}

	// Validate each method in gateway_methods
	for _, method := range p.GatewayMethods {
		err := method.ValidateBasic()
		if err != nil {
			return errors.Wrapf(err, "invalid method in gateway_methods: %s", method.Name)
		}
	}

	return nil
}
