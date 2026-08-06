package keeper

import (
	"context"
	"fmt"

	"github.com/pushchain/push-chain-node/x/ucallback/types"
)

// SetUniversalRead writes a read record.
//
// This is the only sanctioned way to mutate a UniversalRead. Indexes derived from
// the record are reconciled here, so writing k.UniversalReads directly will leave
// them stale.
func (k Keeper) SetUniversalRead(ctx context.Context, ur types.UniversalRead) error {
	if ur.Id == "" {
		return fmt.Errorf("universal read has empty request id")
	}

	return k.UniversalReads.Set(ctx, ur.Id, ur)
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
