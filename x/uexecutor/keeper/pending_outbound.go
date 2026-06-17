package keeper

import (
	"context"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/pushchain/push-chain-node/x/uexecutor/types"
)

// RecordOutboundVote idempotently appends a validator's observation vote
// to the variants list of the existing PendingOutbounds entry. Creates a
// new variant on the first vote of a given (observed_tx bytes / ballotID),
// and appends the voter to an existing variant on subsequent votes for the
// same observation (deduped).
//
// outboundId is the deterministic chain-derived outbound ID.
// ballotID = sha256(utxId:outboundId:marshal(observedTx)) — see GetOutboundBallotKey.
//
// PRECONDITION: PendingOutbounds[outboundId] must already exist — the entry
// is created chain-side at outbound creation in create_outbound.go, well
// before any validator vote arrives. If the entry is missing, this is a
// programmer error and an explicit error is returned.
//
// Multiple variants exist for the same outboundId when validators observe
// different destination-chain results (different success/tx_hash/error/gas).
// The variant data is purely an audit trail — PendingOutbounds entries are
// only removed when validators reach consensus (existing inline removal in
// msg_vote_outbound.go on PASSED). Ballot expiry does NOT remove the entry.
// See plan-pending-outbound-cleanup.md for design rationale.
func (k Keeper) RecordOutboundVote(
	ctx context.Context,
	outboundID string,
	observedTx types.OutboundObservation,
	voter string,
	ballotID string,
) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	height := uint64(sdkCtx.BlockHeight())

	entry, err := k.PendingOutbounds.Get(ctx, outboundID)
	if err != nil {
		return fmt.Errorf("pending outbound entry missing for %s: %w", outboundID, err)
	}

	// Find or create variant for this ballot.
	variantIdx := -1
	for i, v := range entry.Variants {
		if v.BallotId == ballotID {
			variantIdx = i
			break
		}
	}
	if variantIdx < 0 {
		entry.Variants = append(entry.Variants, types.OutboundObservationVariant{
			BallotId:           ballotID,
			ObservedTx:         observedTx,
			Voters:             []string{voter},
			FirstVotedAtHeight: height,
			LastVotedAtHeight:  height,
		})
	} else {
		v := &entry.Variants[variantIdx]
		// Idempotent voter add.
		already := false
		for _, x := range v.Voters {
			if x == voter {
				already = true
				break
			}
		}
		if !already {
			v.Voters = append(v.Voters, voter)
		}
		v.LastVotedAtHeight = height
	}

	k.Logger().Debug("outbound vote recorded",
		"outbound_id", outboundID,
		"ballot_id", ballotID,
		"voter", voter,
		"variant_count", len(entry.Variants),
	)
	return k.PendingOutbounds.Set(ctx, outboundID, entry)
}
