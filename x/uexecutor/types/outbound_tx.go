package types

import (
	"encoding/json"
	"math/big"
	"strings"

	"cosmossdk.io/errors"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/pushchain/push-chain-node/utils"
)

// Stringer method for Params.
func (p OutboundTx) String() string {
	bz, err := json.Marshal(p)
	if err != nil {
		panic(err)
	}

	return string(bz)
}

// ValidateBasic does the sanity check on the OutboundTx fields.
func (p OutboundTx) ValidateBasic() error {
	// Validate destination_chain (must follow CAIP-2 format)
	chain := strings.TrimSpace(p.DestinationChain)
	if chain == "" {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "destination_chain cannot be empty")
	}
	if !strings.Contains(chain, ":") {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "destination_chain must be in CAIP-2 format <namespace>:<reference>")
	}

	// Validate tx_hash (non-empty)
	if strings.TrimSpace(p.TxHash) == "" {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "tx_hash cannot be empty")
	}

	// Validate recipient (non-empty, valid hex address)
	if strings.TrimSpace(p.Recipient) == "" {
		return errors.Wrap(sdkerrors.ErrInvalidAddress, "recipient cannot be empty")
	}
	if !utils.IsValidAddress(p.Recipient, utils.HEX) {
		return errors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid recipient address: %s", p.Recipient)
	}

	// Validate amount as uint256 (non-empty, >0)
	if strings.TrimSpace(p.Amount) == "" {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "amount cannot be empty")
	}
	if bi, ok := new(big.Int).SetString(p.Amount, 10); !ok || bi.Sign() <= 0 {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "amount must be a valid positive uint256")
	}

	// Validate asset_addr (non-empty)
	if strings.TrimSpace(p.AssetAddr) == "" {
		return errors.Wrap(sdkerrors.ErrInvalidAddress, "asset_addr cannot be empty")
	}

	return nil
}
