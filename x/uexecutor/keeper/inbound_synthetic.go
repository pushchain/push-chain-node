package keeper

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/collections"

	"github.com/rollchains/pchain/x/ue/types"
)

// AddInboundSynthetic adds a new inbound synthetic tx to the store
func (k Keeper) AddInboundSynthetic(ctx context.Context, inbound types.InboundSynthetic) error {
	key := types.GetInboundSyntheticKey(inbound.SourceChain, inbound.TxHash, inbound.LogIndex)
	return k.InboundSynthetics.Set(ctx, key, inbound)
}

// GetInboundSynthetic retrieves an inbound synthetic by source chain, tx hash, and log index
func (k Keeper) GetInboundSynthetic(ctx context.Context, sourceChain, txHash, logIndex string) (types.InboundSynthetic, bool, error) {
	key := types.GetInboundSyntheticKey(sourceChain, txHash, logIndex)
	inbound, err := k.InboundSynthetics.Get(ctx, key)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return types.InboundSynthetic{}, false, nil
		}
		return types.InboundSynthetic{}, false, err
	}
	return inbound, true, nil
}

// HasInboundSynthetic checks if an inbound synthetic exists
func (k Keeper) HasInboundSynthetic(ctx context.Context, sourceChain, txHash, logIndex string) (bool, error) {
	key := types.GetInboundSyntheticKey(sourceChain, txHash, logIndex)
	return k.InboundSynthetics.Has(ctx, key)
}

// UpdateInboundSyntheticStatus updates the status of an existing inbound synthetic
func (k Keeper) UpdateInboundSyntheticStatus(ctx context.Context, sourceChain, txHash, logIndex string, status types.Status) error {
	inbound, found, err := k.GetInboundSynthetic(ctx, sourceChain, txHash, logIndex)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("inbound synthetic not found: %s:%s", sourceChain, txHash)
	}

	inbound.InboundStatus = status
	return k.AddInboundSynthetic(ctx, inbound)
}

// IsInboundSyntheticPending checks if the inbound synthetic is pending
func (k Keeper) IsInboundSyntheticPending(ctx context.Context, sourceChain, txHash, logIndex string) (bool, error) {
	inbound, found, err := k.GetInboundSynthetic(ctx, sourceChain, txHash, logIndex)
	if err != nil {
		return false, err
	}
	if !found {
		return false, nil
	}
	return inbound.InboundStatus == types.Status_PENDING, nil
}

// IsInboundSyntheticFinalized checks if the inbound synthetic is finalized
func (k Keeper) IsInboundSyntheticFinalized(ctx context.Context, sourceChain, txHash, logIndex string) (bool, error) {
	inbound, found, err := k.GetInboundSynthetic(ctx, sourceChain, txHash, logIndex)
	if err != nil {
		return false, err
	}
	if !found {
		return false, nil
	}
	return inbound.InboundStatus == types.Status_FINALIZED, nil
}

// RemoveInboundSynthetic removes an inbound synthetic from the store
func (k Keeper) RemoveInboundSynthetic(ctx context.Context, sourceChain, txHash, logIndex string) error {
	key := types.GetInboundSyntheticKey(sourceChain, txHash, logIndex)
	return k.InboundSynthetics.Remove(ctx, key)
}
