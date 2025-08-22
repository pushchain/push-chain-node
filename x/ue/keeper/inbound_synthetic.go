package keeper

import (
	"context"

	"github.com/rollchains/pchain/x/ue/types"
)

// AddPendingInboundSynthetic adds an inbound synthetic to the pending set if not already present
func (k Keeper) AddPendingInboundSynthetic(ctx context.Context, inbound types.InboundSynthetic) error {
	key := types.GetInboundSyntheticKey(inbound)
	has, err := k.PendingInboundSynthetics.Has(ctx, key)
	if err != nil {
		return err
	}
	if has {
		// Already present, do nothing
		return nil
	}
	return k.PendingInboundSynthetics.Set(ctx, key)
}

// IsPendingInboundSynthetic checks if an inbound synthetic is pending
func (k Keeper) IsPendingInboundSynthetic(ctx context.Context, inbound types.InboundSynthetic) (bool, error) {
	key := types.GetInboundSyntheticKey(inbound)
	return k.PendingInboundSynthetics.Has(ctx, key)
}

// RemovePendingInboundSynthetic removes an inbound synthetic from the pending set
func (k Keeper) RemovePendingInboundSynthetic(ctx context.Context, inbound types.InboundSynthetic) error {
	key := types.GetInboundSyntheticKey(inbound)
	return k.PendingInboundSynthetics.Remove(ctx, key)
}
