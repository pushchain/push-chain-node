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

	// recipient must not be empty
	if strings.TrimSpace(p.Recipient) == "" {
		return errors.Wrap(sdkerrors.ErrInvalidAddress, "recipient cannot be empty")
	}

	// sender
	if strings.TrimSpace(p.Sender) == "" {
		return errors.Wrap(sdkerrors.ErrInvalidAddress, "sender cannot be empty")
	}
	if !utils.IsValidAddress(p.Sender, utils.HEX) {
		return errors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid sender address: %s", p.Sender)
	}

	// tx type support
	switch p.TxType {
	case TxType_FUNDS, TxType_FUNDS_AND_PAYLOAD, TxType_PAYLOAD:
		// supported
	default:
		return errors.Wrapf(sdkerrors.ErrInvalidRequest, "unsupported tx_type: %s", p.TxType.String())
	}

	// amount validation (only for funds-related txs)
	if p.TxType == TxType_FUNDS || p.TxType == TxType_FUNDS_AND_PAYLOAD {
		if strings.TrimSpace(p.Amount) == "" {
			return errors.Wrap(sdkerrors.ErrInvalidRequest, "amount cannot be empty for funds tx")
		}
		if bi, ok := new(big.Int).SetString(p.Amount, 10); !ok || bi.Sign() <= 0 {
			return errors.Wrap(sdkerrors.ErrInvalidRequest, "amount must be a valid positive uint256")
		}
	}

	// payload validation (required for payload txs)
	if p.TxType == TxType_PAYLOAD || p.TxType == TxType_FUNDS_AND_PAYLOAD {
		if strings.TrimSpace(p.Payload) == "" {
			return errors.Wrap(sdkerrors.ErrInvalidRequest, "payload cannot be empty for payload tx")
		}
	}

	// asset_addr required when amount is involved
	if (p.TxType == TxType_FUNDS || p.TxType == TxType_FUNDS_AND_PAYLOAD) && strings.TrimSpace(p.AssetAddr) == "" {
		return errors.Wrap(sdkerrors.ErrInvalidAddress, "asset_addr cannot be empty for funds tx")
	}

	// gas_limit (uint)
	if strings.TrimSpace(p.GasLimit) != "" {
		if _, ok := new(big.Int).SetString(p.GasLimit, 10); !ok {
			return errors.Wrap(sdkerrors.ErrInvalidRequest, "gas_limit must be a valid uint")
		}
	}

	// pc_tx validation
	if strings.TrimSpace(p.PcTx.TxHash) == "" {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "pc_tx.tx_hash cannot be empty")
	}
	if strings.TrimSpace(p.PcTx.LogIndex) == "" {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "pc_tx.log_index cannot be empty")
	}

	// observed tx validation (if present)
	if p.ObservedTx != nil {
		if strings.TrimSpace(p.ObservedTx.TxHash) != "" {
			if strings.TrimSpace(p.ObservedTx.DestinationChain) == "" {
				return errors.Wrap(sdkerrors.ErrInvalidRequest, "observed_tx.destination_chain cannot be empty")
			}
			if p.ObservedTx.BlockHeight == 0 {
				return errors.Wrap(sdkerrors.ErrInvalidRequest, "observed_tx.block_height must be > 0")
			}
		}
	}

	// index
	if strings.TrimSpace(p.Index) == "" {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "index cannot be empty")
	}

	return nil
}
