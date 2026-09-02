package types

import (
	"strings"

	"cosmossdk.io/errors"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// ValidateBasic sanity-checks a destination-chain observation. Shared by
// MsgVoteOutbound and MsgExecuteStuckOutbound so the two cannot drift.
func (obs *OutboundObservation) ValidateBasic() error {
	// gas_fee_used is always required — the external chain consumes gas regardless
	// of success or failure, and excess gas must be refundable in both cases.
	if strings.TrimSpace(obs.GasFeeUsed) == "" {
		return errors.Wrap(sdkerrors.ErrInvalidRequest,
			"observed_tx.gas_fee_used is required")
	}
	// Length-capped, range-checked uint256 parse — see F-2026-18798. The value
	// also feeds the outbound ballot key, so a malformed one must never be voted.
	if _, err := ValidateUint256String(obs.GasFeeUsed, "observed_tx.gas_fee_used must be a valid uint256"); err != nil {
		return err
	}

	if obs.Success {
		// Success additionally requires tx_hash and block_height.
		if strings.TrimSpace(obs.TxHash) == "" {
			return errors.Wrap(sdkerrors.ErrInvalidRequest,
				"observed_tx.tx_hash required when success=true")
		}
		if obs.BlockHeight == 0 {
			return errors.Wrap(sdkerrors.ErrInvalidRequest,
				"observed_tx.block_height must be > 0 when success=true")
		}
	} else {
		// Failure case: tx_hash MAY be empty.
		// BUT if tx_hash is present, block_height must be > 0.
		if strings.TrimSpace(obs.TxHash) != "" && obs.BlockHeight == 0 {
			return errors.Wrap(sdkerrors.ErrInvalidRequest,
				"observed_tx.block_height must be > 0 when tx_hash is provided")
		}
	}

	return nil
}
