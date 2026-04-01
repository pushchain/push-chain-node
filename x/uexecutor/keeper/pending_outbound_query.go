package keeper

import (
	"context"

	"github.com/pushchain/push-chain-node/x/uexecutor/types"
)

// HasPendingOutboundsForChain checks if there are any pending outbounds for a given chain.
// It walks PendingOutbounds and joins against UniversalTx to check destination_chain.
// Returns true on first match. This is O(n) but only called during admin-initiated migration.
func (k Keeper) HasPendingOutboundsForChain(ctx context.Context, chain string) (bool, error) {
	var found bool
	err := k.PendingOutbounds.Walk(ctx, nil, func(outboundId string, entry types.PendingOutboundEntry) (bool, error) {
		utx, exists, err := k.GetUniversalTx(ctx, entry.UniversalTxId)
		if err != nil {
			return true, err
		}
		if !exists {
			return false, nil
		}
		for _, ob := range utx.OutboundTx {
			if ob.DestinationChain == chain && ob.Id == outboundId {
				found = true
				return true, nil // stop walking
			}
		}
		return false, nil
	})
	if err != nil {
		return false, err
	}
	return found, nil
}
