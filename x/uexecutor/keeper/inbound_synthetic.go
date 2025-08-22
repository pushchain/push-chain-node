package keeper

import (
	"context"
	"errors"

	"cosmossdk.io/collections"

	"github.com/rollchains/pchain/x/ue/types"
)

// AddInboundSynthetic adds a new inbound synthetic status to the store
func (k Keeper) AddInboundSynthetic(ctx context.Context, inbound types.InboundSynthetic, status types.Status) error {
	key := types.GetInboundSyntheticKey(inbound)
	return k.InboundSynthetics.Set(ctx, key, types.InboundStatus{Status: status})
}

// GetInboundSyntheticStatus retrieves the status of an inbound synthetic by its key
func (k Keeper) GetInboundSyntheticStatus(ctx context.Context, inbound types.InboundSynthetic) (types.Status, bool, error) {
	key := types.GetInboundSyntheticKey(inbound)
	inboundStatus, err := k.InboundSynthetics.Get(ctx, key)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return types.Status_UNSPECIFIED, false, nil
		}
		return types.Status_UNSPECIFIED, false, err
	}
	return inboundStatus.Status, true, nil
}

// UpdateInboundSyntheticStatus updates the status of an existing inbound synthetic
func (k Keeper) UpdateInboundSyntheticStatus(ctx context.Context, inbound types.InboundSynthetic, status types.Status) error {
	key := types.GetInboundSyntheticKey(inbound)
	return k.InboundSynthetics.Set(ctx, key, types.InboundStatus{Status: status})
}

// IsInboundSyntheticPending checks if the inbound synthetic is pending
func (k Keeper) IsInboundSyntheticPending(ctx context.Context, inbound types.InboundSynthetic) (bool, error) {
	status, found, err := k.GetInboundSyntheticStatus(ctx, inbound)
	if err != nil || !found {
		return false, err
	}
	return status == types.Status_PENDING, nil
}

// IsInboundSyntheticFinalized checks if the inbound synthetic is finalized
func (k Keeper) IsInboundSyntheticFinalized(ctx context.Context, inbound types.InboundSynthetic) (bool, error) {
	status, found, err := k.GetInboundSyntheticStatus(ctx, inbound)
	if err != nil || !found {
		return false, err
	}
	return status == types.Status_FINALIZED, nil
}

// RemoveInboundSynthetic removes an inbound synthetic status from the store
func (k Keeper) RemoveInboundSynthetic(ctx context.Context, inbound types.InboundSynthetic) error {
	key := types.GetInboundSyntheticKey(inbound)
	return k.InboundSynthetics.Remove(ctx, key)
}
