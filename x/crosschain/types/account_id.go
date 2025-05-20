package types

import (
	"encoding/hex"
	"encoding/json"
	"strings"

	"cosmossdk.io/errors"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// Stringer method for Params.
func (p AccountId) String() string {
	bz, err := json.Marshal(p)
	if err != nil {
		panic(err)
	}

	return string(bz)
}

// Validate does the sanity check on the params.
func (p AccountId) Validate() error {

	// Validate namespace is non-empty
	if len(p.Namespace) == 0 {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "namespace cannot be empty")
	}

	// Validate chainId is non-empty
	if len(p.ChainId) == 0 {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "chainId cannot be empty")
	}

	// Validate data (hex string)
	if len(p.OwnerKey) > 0 {
		if _, err := hex.DecodeString(strings.TrimPrefix(p.OwnerKey, "0x")); err != nil {
			return errors.Wrap(sdkerrors.ErrInvalidRequest, "invalid hex data")
		}
	}

	// Ensure vm_type is within the known enum range
	if p.VmType < 0 || int(p.VmType) > int(VM_TYPE_OTHER_VM) {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "invalid vm_type")
	}

	return nil
}
