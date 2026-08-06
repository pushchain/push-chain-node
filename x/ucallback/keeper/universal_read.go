package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/collections"

	"github.com/pushchain/push-chain-node/x/ucallback/types"
)

// isSettled reports whether a read has reached a terminal state and should no
// longer be swept for expiry.
func isSettled(s types.UniversalReadStatus) bool {
	switch s {
	case types.UniversalReadStatus_UNIVERSAL_READ_STATUS_FULFILLED,
		types.UniversalReadStatus_UNIVERSAL_READ_STATUS_EXPIRED,
		types.UniversalReadStatus_UNIVERSAL_READ_STATUS_FAILED:
		return true
	default:
		return false
	}
}

// SetUniversalRead writes a read record.
//
// This is the only sanctioned way to mutate a UniversalRead. Indexes derived from
// the record are reconciled here, so writing k.UniversalReads directly will leave
// them stale — in particular the sweeper would keep expiring a read that has
// already settled.
func (k Keeper) SetUniversalRead(ctx context.Context, ur types.UniversalRead) error {
	if ur.Id == "" {
		return fmt.Errorf("universal read has empty request id")
	}

	if err := k.UniversalReads.Set(ctx, ur.Id, ur); err != nil {
		return err
	}

	// pending-by-expiry: present only while unsettled
	if ur.Request != nil {
		key := collections.Join(ur.Request.ExpiryBlockHeight, ur.Id)
		if isSettled(ur.Status) {
			if err := k.PendingByExpiry.Remove(ctx, key); err != nil {
				return err
			}
		} else if err := k.PendingByExpiry.Set(ctx, key); err != nil {
			return err
		}

		// reads-by-tx: written once, never removed — it is provenance, not state
		if ur.Request.RequestedTxHash != "" {
			if err := k.ReadsByTxHash.Set(ctx,
				collections.Join(ur.Request.RequestedTxHash, ur.Id)); err != nil {
				return err
			}
		}
	}

	return nil
}

// GetUniversalRead returns the read for requestId, if it exists.
func (k Keeper) GetUniversalRead(ctx context.Context, requestID string) (types.UniversalRead, bool) {
	return k.getUniversalReadRaw(ctx, requestID)
}

func (k Keeper) getUniversalReadRaw(ctx context.Context, requestID string) (types.UniversalRead, bool) {
	ur, err := k.UniversalReads.Get(ctx, requestID)
	if err != nil {
		return types.UniversalRead{}, false
	}
	return ur, true
}

// HasUniversalRead reports whether a read already exists. Ingest uses this to
// stay idempotent when the same log is seen twice.
func (k Keeper) HasUniversalRead(ctx context.Context, requestID string) bool {
	has, err := k.UniversalReads.Has(ctx, requestID)
	return err == nil && has
}

// IterateExpiredBy calls fn for every unsettled read whose expiry height is at or
// below height, in ascending height order. The sweeper drives this.
//
// The key codec orders by expiryHeight first, so a plain ascending walk reaches
// every due entry before any that is not yet due — we break at the first key past
// height rather than constructing a cross-prefix range.
func (k Keeper) IterateExpiredBy(ctx context.Context, height uint64, fn func(types.UniversalRead) bool) error {
	iter, err := k.PendingByExpiry.Iterate(ctx, nil)
	if err != nil {
		return err
	}
	defer iter.Close()

	for ; iter.Valid(); iter.Next() {
		key, err := iter.Key()
		if err != nil {
			return err
		}
		if key.K1() > height {
			break
		}
		ur, found := k.getUniversalReadRaw(ctx, key.K2())
		if !found {
			continue
		}
		if !fn(ur) {
			return nil
		}
	}
	return nil
}

// GetUniversalReadByBallot resolves a ballot key to its read. AfterBallotTerminal
// hands us only a ballot ID, and ballot IDs are one-way digests over the
// observation — not reversible — so this scans rather than indexes.
//
// The scan is over PendingByExpiry, not UniversalReads: entries leave that set the
// moment a read settles, so it holds only in-flight work. This mirrors uexecutor's
// ballot hook, which walks PendingInbounds for the same reason
// (x/uexecutor/keeper/ballot_hooks.go:86) — the pending set is small and transient,
// and this path only runs on terminal transitions.
//
// Returns false if no pending read owns the ballot: it may have already settled by
// another path, or the ballot may not belong to this module at all.
func (k Keeper) GetUniversalReadByBallot(ctx context.Context, ballotKey string) (types.UniversalRead, bool) {
	if ballotKey == "" {
		return types.UniversalRead{}, false
	}

	var (
		found types.UniversalRead
		ok    bool
	)
	err := k.PendingByExpiry.Walk(ctx, nil, func(key collections.Pair[uint64, string]) (bool, error) {
		ur, exists := k.getUniversalReadRaw(ctx, key.K2())
		if exists && ur.BallotKey == ballotKey {
			found, ok = ur, true
			return true, nil
		}
		return false, nil
	})
	if err != nil {
		return types.UniversalRead{}, false
	}
	return found, ok
}

// IterateReadsByTxHash calls fn for every read requested by the given Push tx.
// A single transaction can emit several ReadRequested logs; each is its own
// record, and this is how the batch is reassembled.
func (k Keeper) IterateReadsByTxHash(ctx context.Context, txHash string, fn func(types.UniversalRead) bool) error {
	rng := collections.NewPrefixedPairRange[string, string](txHash)
	iter, err := k.ReadsByTxHash.Iterate(ctx, rng)
	if err != nil {
		return err
	}
	defer iter.Close()

	for ; iter.Valid(); iter.Next() {
		key, err := iter.Key()
		if err != nil {
			return err
		}
		ur, found := k.getUniversalReadRaw(ctx, key.K2())
		if !found {
			continue
		}
		if !fn(ur) {
			return nil
		}
	}
	return nil
}
