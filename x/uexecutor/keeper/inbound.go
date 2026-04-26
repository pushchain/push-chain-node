package keeper

import (
	"context"

	"github.com/pushchain/push-chain-node/x/uexecutor/types"
)

// AddPendingInbound adds an inbound synthetic to the pending set if not already present
func (k Keeper) AddPendingInbound(ctx context.Context, inbound types.Inbound) error {
	key := types.GetInboundUniversalTxKey(inbound)
	has, err := k.PendingInbounds.Has(ctx, key)
	if err != nil {
		return err
	}
	if has {
		// Already present, do nothing
		k.Logger().Debug("add pending inbound skipped: already present", "utx_key", key)
		return nil
	}
	k.Logger().Debug("pending inbound added", "utx_key", key, "source_chain", inbound.SourceChain)
	return k.PendingInbounds.Set(ctx, key)
}

// IsPendingInbound checks if an inbound synthetic is pending
func (k Keeper) IsPendingInbound(ctx context.Context, inbound types.Inbound) (bool, error) {
	key := types.GetInboundUniversalTxKey(inbound)
	return k.PendingInbounds.Has(ctx, key)
}

// RemovePendingInbound removes an inbound synthetic from the pending set
func (k Keeper) RemovePendingInbound(ctx context.Context, inbound types.Inbound) error {
	key := types.GetInboundUniversalTxKey(inbound)
	k.Logger().Debug("pending inbound removed", "utx_key", key)
	return k.PendingInbounds.Remove(ctx, key)
}
