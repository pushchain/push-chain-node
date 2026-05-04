package keeper

import (
	"context"
	"encoding/hex"
	"errors"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/pushchain/push-chain-node/x/uexecutor/types"
	uvalidatortypes "github.com/pushchain/push-chain-node/x/uvalidator/types"
)

// BallotHooks is the x/uexecutor implementation of x/uvalidator's
// BallotHooks interface. It reacts to ballot lifecycle terminal
// transitions (EXPIRED, PASSED, REJECTED) by maintaining the per-variant
// audit trail in PendingInbounds and (on terminal-failure) routing
// expired entries to ExpiredInbounds for the future escape-hatch flow.
//
// Currently only INBOUND_TX ballots are handled. OUTBOUND_TX ballots are
// intentionally NOT handled here — outbound PendingOutbounds entries
// persist until validators reach consensus (existing inline removal in
// msg_vote_outbound.go on PASSED). Operators investigate stuck outbounds
// by correlating each variant's ballot_id with the uvalidator ballot
// status separately. See plan-pending-outbound-cleanup.md for rationale.
type BallotHooks struct {
	k Keeper
}

// NewBallotHooks constructs the BallotHooks implementation backed by the
// given Keeper.
func NewBallotHooks(k Keeper) BallotHooks {
	return BallotHooks{k: k}
}

var _ uvalidatortypes.BallotHooks = BallotHooks{}

// AfterBallotTerminal is invoked by x/uvalidator when a ballot reaches a
// terminal state. For INBOUND_TX ballots this:
//
//  1. Marks the matching variant in the PendingInbounds entry with the
//     terminal status that was reached.
//  2. If ANY variant is still PENDING, persists the updated entry and
//     returns — the entry continues to wait on the remaining ballot(s).
//  3. If ALL variants are now terminal:
//     a. Removes the entry from PendingInbounds.
//     b. If any variant ended PASSED, the existing post-finalization path
//        in VoteInbound has already produced a UniversalTx — nothing more
//        to do.
//     c. If ALL variants ended EXPIRED/REJECTED (no UTX was ever created),
//        copies the entry into ExpiredInbounds preserving the full
//        per-variant audit trail for the future escape-hatch refund flow.
//
// Hook implementations are required to be idempotent and must not block
// the terminal transition by returning errors for non-fatal conditions.
// Decode failures and "entry already cleared" cases are warning-logged
// and swallowed.
func (h BallotHooks) AfterBallotTerminal(
	ctx sdk.Context,
	ballotID string,
	ballotType uvalidatortypes.BallotObservationType,
	status uvalidatortypes.BallotStatus,
) error {
	switch ballotType {
	case uvalidatortypes.BallotObservationType_BALLOT_OBSERVATION_TYPE_INBOUND_TX:
		return h.afterInboundBallotTerminal(ctx, ballotID, status)
	default:
		// OUTBOUND_TX, TSS_KEY, FUND_MIGRATION — not handled here.
		// See doc comment on BallotHooks for rationale on outbound.
		return nil
	}
}

func (h BallotHooks) afterInboundBallotTerminal(
	ctx context.Context,
	ballotID string,
	status uvalidatortypes.BallotStatus,
) error {
	// Decode ballot ID → Inbound (ballot ID for INBOUND_TX is hex(marshal(Inbound))).
	bz, err := hex.DecodeString(ballotID)
	if err != nil {
		h.k.Logger().Warn("ballot terminal hook: cannot hex-decode inbound ballot id",
			"ballot_id", ballotID, "err", err.Error())
		return nil
	}
	var inbound types.Inbound
	if err := inbound.Unmarshal(bz); err != nil {
		h.k.Logger().Warn("ballot terminal hook: cannot unmarshal inbound from ballot id",
			"ballot_id", ballotID, "err", err.Error())
		return nil
	}
	utxKey := types.GetInboundUniversalTxKey(inbound)

	entry, err := h.k.PendingInbounds.Get(ctx, utxKey)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			// Entry was already cleared (e.g. the consensus-success path in
			// VoteInbound already removed it before this hook fires). Nothing
			// to do.
			return nil
		}
		return err
	}

	// Mark this variant's terminal status.
	found := false
	for i := range entry.Variants {
		if entry.Variants[i].BallotId == ballotID {
			entry.Variants[i].TerminalStatus = status
			found = true
			break
		}
	}
	if !found {
		h.k.Logger().Warn("ballot terminal hook: inbound variant not found in pending entry",
			"ballot_id", ballotID, "utx_key", utxKey)
		return nil
	}

	// If any variant is still PENDING, persist the updated entry and wait.
	for _, v := range entry.Variants {
		if v.TerminalStatus == uvalidatortypes.BallotStatus_BALLOT_STATUS_PENDING {
			return h.k.PendingInbounds.Set(ctx, utxKey, entry)
		}
	}

	// All variants terminal. Remove from pending.
	if err := h.k.PendingInbounds.Remove(ctx, utxKey); err != nil {
		return err
	}

	// If any variant PASSED, the existing post-finalization path in
	// VoteInbound has produced (or will produce) a UniversalTx — nothing
	// to route to ExpiredInbounds.
	for _, v := range entry.Variants {
		if v.TerminalStatus == uvalidatortypes.BallotStatus_BALLOT_STATUS_PASSED {
			return nil
		}
	}

	// All variants are terminal-failure (EXPIRED or REJECTED). Preserve
	// the full audit trail in ExpiredInbounds for the future escape-hatch
	// refund flow.
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	return h.k.ExpiredInbounds.Set(ctx, utxKey, types.ExpiredInboundEntry{
		UtxKey:          utxKey,
		Variants:        entry.Variants,
		ExpiredAtHeight: uint64(sdkCtx.BlockHeight()),
	})
}
