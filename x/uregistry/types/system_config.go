package types

import (
	"encoding/json"
	"strings"

	"cosmossdk.io/errors"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/pushchain/push-chain-node/utils"
)

// Stringer method for Params.
func (s SystemConfig) String() string {
	bz, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}

	return string(bz)
}

// Validate does the sanity check on the system config params.
func (s SystemConfig) ValidateBasic() error {
	if strings.TrimSpace(s.UniversalCoreAddress) == "" {
		return errors.Wrap(sdkerrors.ErrInvalidAddress, "universal core address cannot be empty")
	}
	if !utils.IsValidAddress(s.UniversalCoreAddress, utils.HEX) {
		return errors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid universal core address: %s", s.UniversalCoreAddress)
	}

	return nil
}
