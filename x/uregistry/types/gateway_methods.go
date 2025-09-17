package types

import (
	"encoding/hex"
	"encoding/json"
	"strings"

	"cosmossdk.io/errors"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// Stringer method for Params.
func (p GatewayMethods) String() string {
	bz, err := json.Marshal(p)
	if err != nil {
		panic(err)
	}

	return string(bz)
}

// Validate does the sanity check on the params.
func (p GatewayMethods) ValidateBasic() error {
	// Name must not be empty
	if strings.TrimSpace(p.Name) == "" {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "method name cannot be empty")
	}

	//Identifier must not be empty and must be valid hex
	if strings.TrimSpace(p.Identifier) == "" {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "method identifier cannot be empty")
	}
	if _, err := hex.DecodeString(strings.TrimPrefix(p.Identifier, "0x")); err != nil {
		return errors.Wrapf(sdkerrors.ErrInvalidRequest, "method selector must be valid hex: %s", err.Error())
	}

	// Event Identifier must not be empty and must be valid hex
	if strings.TrimSpace(p.EventIdentifier) == "" {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "method event_identifier cannot be empty")
	}
	if _, err := hex.DecodeString(strings.TrimPrefix(p.EventIdentifier, "0x")); err != nil {
		return errors.Wrapf(sdkerrors.ErrInvalidRequest, "method event_identifier must be valid hex: %s", err.Error())
	}

	// ConfirmationType must be known
	switch p.ConfirmationType {
	case ConfirmationType_CONFIRMATION_TYPE_STANDARD,
		ConfirmationType_CONFIRMATION_TYPE_FAST:
		// valid
	default:
		return errors.Wrapf(sdkerrors.ErrInvalidRequest, "invalid confirmation type: %v", p.ConfirmationType)
	}

	return nil
}
