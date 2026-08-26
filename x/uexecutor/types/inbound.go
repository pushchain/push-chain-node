package types

import (
	"encoding/json"
	"fmt"
	"strings"

	"cosmossdk.io/errors"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/pushchain/push-chain-node/utils"
)

const EvmZeroAddress = "0x0000000000000000000000000000000000000000"

// Canonicalize normalizes encoding-variant fields in place (per source-chain
// namespace) so the same event from any observer is byte-identical across
// ballot keys, UTX keys and registry lookups. Lenient (unparseable values are
// kept trimmed, never rejected) because the vote path must always record a
// UTX — execution-level validation rejects malformed inbounds later.
func (p *Inbound) Canonicalize() {
	p.SourceChain = strings.TrimSpace(p.SourceChain)
	p.TxHash = utils.LenientCanonicalizeTxHash(p.SourceChain, p.TxHash)
	p.Sender = utils.LenientCanonicalizeAddress(p.SourceChain, p.Sender)
	p.AssetAddr = utils.LenientCanonicalizeAddress(p.SourceChain, p.AssetAddr)
	// Recipient lives on Push Chain (EVM) regardless of source chain.
	p.Recipient = utils.LenientCanonicalizeEVMAddress(p.Recipient)
	p.LogIndex = strings.TrimSpace(p.LogIndex)
	p.Amount = strings.TrimSpace(p.Amount)
	p.RawPayload = utils.CanonicalizeHexBlob(p.RawPayload)
	p.VerificationData = utils.CanonicalizeHexBlob(p.VerificationData)
	if p.RevertInstructions != nil {
		// Refunds return to the source chain.
		p.RevertInstructions.FundRecipient = utils.LenientCanonicalizeAddress(p.SourceChain, p.RevertInstructions.FundRecipient)
	}
}

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

// ValidateSize enforces MaxUniversalPayloadBytes on every variable-length
// payload field an inbound carries. raw_payload is the wire form of the
// universal payload and universal_payload is what a validator submits before
// the core decodes raw_payload itself; both land in PendingInbounds state on
// the first vote, on a fee-exempt msg, so both are bounded here.
//
// Split out of ValidateBasic so the keeper can apply the cap on its own: a
// universal validator submits votes wrapped in authz.MsgExec
// (universalClient/pushsigner/pushsigner.go wrapWithAuthZ), which baseapp does
// not validate at CheckTx, so the cap must not depend on one call site.
func (p *Inbound) ValidateSize() error {
	if p == nil {
		return nil
	}
	if err := ValidatePayloadBlobSize("raw_payload", p.RawPayload); err != nil {
		return err
	}
	if err := ValidatePayloadBlobSize("verification_data", p.VerificationData); err != nil {
		return err
	}
	return p.UniversalPayload.ValidateSize()
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
	// Reject oversized payload blobs before anything else: unlike the
	// execution-level checks below, this one is a resource bound, and the bytes
	// are already in the block by the time execution validation runs.
	if err := p.ValidateSize(); err != nil {
		return err
	}

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
	// Length-capped, range-checked uint256 parse — see F-2026-18798.
	bi, err := ValidateUint256String(p.Amount, "amount must be a valid non-negative uint256")
	if err != nil {
		return err
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
