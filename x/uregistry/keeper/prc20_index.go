package keeper

import (
	"context"

	"github.com/pushchain/push-chain-node/x/uregistry/types"
)

// RebuildPRC20Index repopulates the PRC20 secondary index from the primary
// TokenConfigs map. Pre-upgrade nodes wrote token rows before the PRC20Index
// existed, so on upgrade the primary store is full while the index is empty.
// The upgrade migration calls this to backfill it: re-Setting each row re-runs
// the index function. It is idempotent (Set removes the stale index ref before
// writing the new one) and a no-op on already-indexed or empty state.
func (k Keeper) RebuildPRC20Index(ctx context.Context) error {
	// Collect keys first — mutating the IndexedMap while walking it would
	// invalidate the iterator.
	var keys []string
	if err := k.TokenConfigs.Walk(ctx, nil, func(key string, _ types.TokenConfig) (bool, error) {
		keys = append(keys, key)
		return false, nil
	}); err != nil {
		return err
	}

	for _, key := range keys {
		cfg, err := k.TokenConfigs.Get(ctx, key)
		if err != nil {
			return err
		}
		if err := k.TokenConfigs.Set(ctx, key, cfg); err != nil {
			return err
		}
	}
	return nil
}
