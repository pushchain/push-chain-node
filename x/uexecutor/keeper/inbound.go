package keeper

import (
	"context"
	"errors"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/pushchain/push-chain-node/x/uexecutor/types"
	uvalidatortypes "github.com/pushchain/push-chain-node/x/uvalidator/types"
)

// RecordInboundVote idempotently records a validator's vote on an inbound by
// appending to the per-utx PendingInbounds entry. Creates the entry on the
// first vote for a given utx_key, creates a new variant on the first vote
// of a given (inbound payload bytes / ballotID), and appends the voter to
// an existing variant on subsequent votes for the same payload (deduped).
//
// utx_key = sha256(source_chain:tx_hash:log_index) — see GetInboundUniversalTxKey.
// ballotID = hex(marshal(Inbound)) — see GetInboundBallotKey.
//
// Multiple variants exist for the same utx_key when validators marshal
// slightly different Inbound bytes for the same logical event (different
// decoded fields, formatting, etc.). Each variant tracks which validators
// voted for that exact byte sequence so operators can investigate divergence.
func (k Keeper) RecordInboundVote(
	ctx context.Context,
	inbound types.Inbound,
	voter string,
	ballotID string,
) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	height := uint64(sdkCtx.BlockHeight())
	utxKey := types.GetInboundUniversalTxKey(inbound)

	entry, err := k.PendingInbounds.Get(ctx, utxKey)
	if err != nil && !errors.Is(err, collections.ErrNotFound) {
		return err
	}
	if errors.Is(err, collections.ErrNotFound) {
		entry = types.PendingInboundEntry{
			UtxKey:          utxKey,
			CreatedAtHeight: height,
		}
	}

	// Find or create the variant for this ballot.
	variantIdx := -1
	for i, v := range entry.Variants {
		if v.BallotId == ballotID {
			variantIdx = i
			break
		}
	}
	if variantIdx < 0 {
		entry.Variants = append(entry.Variants, types.InboundVariant{
			BallotId:           ballotID,
			Inbound:            &inbound,
			Voters:             []string{voter},
			FirstVotedAtHeight: height,
			LastVotedAtHeight:  height,
			TerminalStatus:     uvalidatortypes.BallotStatus_BALLOT_STATUS_PENDING,
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

	k.Logger().Debug("inbound vote recorded",
		"utx_key", utxKey,
		"ballot_id", ballotID,
		"voter", voter,
		"variant_count", len(entry.Variants),
	)
	return k.PendingInbounds.Set(ctx, utxKey, entry)
}

// IsPendingInbound reports whether any variant for this inbound's utx_key
// is still being tracked (any entry exists in PendingInbounds).
func (k Keeper) IsPendingInbound(ctx context.Context, inbound types.Inbound) (bool, error) {
	utxKey := types.GetInboundUniversalTxKey(inbound)
	return k.PendingInbounds.Has(ctx, utxKey)
}

// RemovePendingInbound removes the per-utx entry. The variant-aware design
// only needs this on the consensus-success path inside VoteInbound (the
// BallotHooks impl in ballot_hooks.go performs the same removal when ALL
// variants reach a terminal state). Map.Remove on absent key is a no-op.
func (k Keeper) RemovePendingInbound(ctx context.Context, inbound types.Inbound) error {
	utxKey := types.GetInboundUniversalTxKey(inbound)
	k.Logger().Debug("pending inbound removed", "utx_key", utxKey)
	return k.PendingInbounds.Remove(ctx, utxKey)
}
