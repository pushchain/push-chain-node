package types

import (
	"encoding/json"
	"strings"

	"cosmossdk.io/errors"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/rollchains/pchain/utils"
)

// Stringer method for Params.
func (p PCTx) String() string {
	bz, err := json.Marshal(p)
	if err != nil {
		panic(err)
	}

	return string(bz)
}

// ValidateBasic does the sanity check on the PCTx fields.
func (p PCTx) ValidateBasic() error {
	// Validate tx_hash (non-empty, valid hash)
	if strings.TrimSpace(p.TxHash) == "" {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "tx_hash cannot be empty")
	}

	// Validate sender (non-empty, valid hex address)
	if strings.TrimSpace(p.Sender) == "" {
		return errors.Wrap(sdkerrors.ErrInvalidAddress, "sender cannot be empty")
	}
	if !utils.IsValidAddress(p.Sender, utils.HEX) {
		return errors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid sender address: %s", p.Sender)
	}

	// Validate block_height
	if p.BlockHeight == 0 {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "block_height must be greater than zero")
	}

	// Validate status
	status := strings.ToUpper(strings.TrimSpace(p.Status))
	if status != "SUCCESS" && status != "FAILED" {
		return errors.Wrapf(sdkerrors.ErrInvalidRequest, "status must be either SUCCESS or FAILED, got: %s", p.Status)
	}

	return nil
}
