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

	// Validate namespace is non-empty
	if len(p.Namespace) == 0 {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "namespace cannot be empty")
	}

	// Validate chainId is non-empty
	if len(p.ChainId) == 0 {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "chainId cannot be empty")
	}

	// Validate publicRpcUrl is non-empty
	if len(p.PublicRpcUrl) == 0 {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "public_rpc_url cannot be empty")
	}

	// Validate lockerContractAddress is non-empty
	if len(p.LockerContractAddress) == 0 {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "locker_contract_address cannot be empty")
	}

	// Validate fundsAddedEventTopic is non-empty
	if len(p.FundsAddedEventTopic) == 0 {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "funds_added_event_topic cannot be empty")
	}

	// Ensure vm_type is within the known enum range
	if p.VmType < 0 || int(p.VmType) > int(VM_TYPE_OTHER_VM) {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "invalid vm_type")
	}

	return nil
}
