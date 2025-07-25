package types

import (
	"encoding/json"
	"strings"

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
	// Validate chain is non-empty and follows CAIP-2 format
	chain := strings.TrimSpace(p.Chain)
	if chain == "" {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "chain cannot be empty")
	}
	if !strings.Contains(chain, ":") {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "chain must be in CAIP-2 format <namespace>:<reference>")
	}

	// Validate publicRpcUrl
	if strings.TrimSpace(p.PublicRpcUrl) == "" {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "public_rpc_url cannot be empty")
	}

	// Validate gatewayAddress
	if strings.TrimSpace(p.GatewayAddress) == "" {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "gateway_address cannot be empty")
	}

	// Validate vm_type is within known enum range
	if _, ok := VmType_name[int32(p.VmType)]; !ok {
		return errors.Wrapf(sdkerrors.ErrInvalidRequest, "invalid vm_type: %v", p.VmType)
	}

	// Validate gateway methods
	if len(p.GatewayMethods) == 0 {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "gateway_methods cannot be empty")
	}

	for _, method := range p.GatewayMethods {
		if err := method.ValidateBasic(); err != nil {
			return errors.Wrapf(err, "invalid method in gateway_methods: %s", method.Name)
		}
	}

	return p.BlockConfirmation.ValidateBasic()
}
