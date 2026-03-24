package types

import (
	"encoding/json"
	"fmt"
	"math/big"
	"strings"

	"cosmossdk.io/errors"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/pushchain/push-chain-node/utils"
)

const EvmZeroAddress = "0x0000000000000000000000000000000000000000"

// NormalizeForTxType zeroes out fields that are irrelevant for the given TxType,
// and decodes raw_payload into universal_payload for payload types.
// This should be called by the core module after ballot finalization.
// Returns an error if raw_payload decoding fails.
func (p *Inbound) NormalizeForTxType() error {
	switch p.TxType {
	case TxType_FUNDS_AND_PAYLOAD, TxType_GAS_AND_PAYLOAD:
		// Payload types: recipient is only meaningful when isCEA
		if !p.IsCEA {
			p.Recipient = EvmZeroAddress
		}
		// Always clear universal_payload — whatever the UV sends is ignored.
		// Core validator decodes from raw_payload.
		p.UniversalPayload = nil

		// Decode raw_payload → universal_payload
		if p.RawPayload != "" {
			decoded, err := DecodeRawPayload(p.RawPayload, p.SourceChain)
			if err != nil {
				return fmt.Errorf("failed to decode raw payload: %w", err)
			}
			if decoded == nil {
				return fmt.Errorf("raw_payload decoded to nil for payload tx type")
			}
			p.UniversalPayload = decoded
			p.RawPayload = "" // clear after successful decode to save storage
		}
	default:
		// Non-payload types: payload is not used
		p.UniversalPayload = nil
		p.VerificationData = ""
		p.RawPayload = ""
	}
	return nil
}

// Stringer method for Params.
func (p Inbound) String() string {
	bz, err := json.Marshal(p)
	if err != nil {
		panic(err)
	}

	return string(bz)
}

// ValidateBasic does minimal sanity checks needed to accept a vote.
// Only fields required to identify the inbound and create a UTX key are validated here.
// Execution-level validation (amount, addresses, payload, recipient) is deferred to
// ValidateForExecution so that invalid inbounds still produce an on-chain UTX record
// (with a failed PCTx / revert) instead of silently dropping the vote and leaving
// user funds stuck in the gateway.
func (p Inbound) ValidateBasic() error {
	// Validate source_chain (must follow CAIP-2 format) — needed for UTX key
	chain := strings.TrimSpace(p.SourceChain)
	if chain == "" {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "source chain cannot be empty")
	}
	if !strings.Contains(chain, ":") {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "source chain must be in CAIP-2 format <namespace>:<reference>")
	}

	// Validate tx_hash — needed for UTX key
	if strings.TrimSpace(p.TxHash) == "" {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "tx_hash cannot be empty")
	}

	// Validate sender — needed for revert recipient fallback
	if strings.TrimSpace(p.Sender) == "" {
		return errors.Wrap(sdkerrors.ErrInvalidAddress, "sender cannot be empty")
	}

	// Validate log_index — needed for UTX key
	if strings.TrimSpace(p.LogIndex) == "" {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "log_index cannot be empty")
	}

	// Validate tx_type enum — needed to route execution
	if _, ok := TxType_name[int32(p.TxType)]; !ok || p.TxType == TxType_UNSPECIFIED_TX {
		return errors.Wrapf(sdkerrors.ErrInvalidRequest, "invalid tx_type: %v", p.TxType)
	}

	return nil
}

// ValidateForExecution checks fields that are required for actual execution of the inbound.
// Called after ballot finalization, before ExecuteInbound. Failures here produce a failed
// PCTx and (for non-isCEA) a revert outbound, rather than dropping the vote.
func (p Inbound) ValidateForExecution() error {
	// Validate amount as uint256
	if strings.TrimSpace(p.Amount) == "" {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "amount cannot be empty")
	}
	bi, ok := new(big.Int).SetString(p.Amount, 10)
	if !ok || bi.Sign() < 0 {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "amount must be a valid non-negative uint256")
	}
	// Only GAS_AND_PAYLOAD and FUNDS_AND_PAYLOAD allow zero amount (skip deposit, still execute payload)
	if bi.Sign() == 0 && p.TxType != TxType_GAS_AND_PAYLOAD && p.TxType != TxType_FUNDS_AND_PAYLOAD {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "amount must be positive for this tx type")
	}

	// Validate asset_addr
	if strings.TrimSpace(p.AssetAddr) == "" {
		return errors.Wrap(sdkerrors.ErrInvalidAddress, "asset_addr cannot be empty")
	}

	// isCEA is only supported for FUNDS, FUNDS_AND_PAYLOAD, and GAS_AND_PAYLOAD
	if p.IsCEA && p.TxType != TxType_FUNDS && p.TxType != TxType_FUNDS_AND_PAYLOAD && p.TxType != TxType_GAS_AND_PAYLOAD {
		return errors.Wrapf(sdkerrors.ErrInvalidRequest, "isCEA is only supported for FUNDS, FUNDS_AND_PAYLOAD, and GAS_AND_PAYLOAD tx types, got: %v", p.TxType)
	}

	// Validate fields required per tx_type
	switch p.TxType {
	case TxType_FUNDS_AND_PAYLOAD, TxType_GAS_AND_PAYLOAD:
		if p.UniversalPayload == nil {
			return errors.Wrap(sdkerrors.ErrInvalidRequest, "payload is required for payload tx types")
		}
		if p.IsCEA && strings.TrimSpace(p.Recipient) == "" {
			return errors.Wrap(sdkerrors.ErrInvalidAddress, "recipient cannot be empty when isCEA is true")
		}
		if p.IsCEA && !utils.IsValidAddress(p.Recipient, utils.HEX) {
			return errors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid recipient address when isCEA is true: %s", p.Recipient)
		}
		if err := p.UniversalPayload.ValidateBasic(); err != nil {
			return errors.Wrap(err, "invalid payload")
		}
	case TxType_FUNDS, TxType_GAS:
		if strings.TrimSpace(p.Recipient) == "" {
			return errors.Wrap(sdkerrors.ErrInvalidAddress, "recipient cannot be empty")
		}
		if !utils.IsValidAddress(p.Recipient, utils.HEX) {
			return errors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid recipient address: %s", p.Recipient)
		}
	}

	return nil
}
