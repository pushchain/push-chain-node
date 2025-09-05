package keeper

import (
	"context"
	"errors"
	"fmt"

	// sdk "github.com/cosmos/cosmos-sdk/types"
	"cosmossdk.io/collections"
	"github.com/pushchain/push-chain-node/x/uexecutor/types"
)

// CreateUniversalTx stores a new UniversalTx
func (k Keeper) CreateUniversalTx(ctx context.Context, key string, utx types.UniversalTx) error {
	exists, err := k.UniversalTx.Has(ctx, key)
	if err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("universal tx with key %s already exists", key)
	}

	return k.UniversalTx.Set(ctx, key, utx)
}

// GetUniversalTx retrieves a UniversalTx by key, returns (value, found, error)
func (k Keeper) GetUniversalTx(ctx context.Context, key string) (types.UniversalTx, bool, error) {
	utx, err := k.UniversalTx.Get(ctx, key)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return types.UniversalTx{}, false, nil
		}
		return types.UniversalTx{}, false, err
	}
	return utx, true, nil
}

// UpdateUniversalTx updates an existing UniversalTx in the store.
// It fetches the UniversalTx, applies the update function, and saves it back.
func (k Keeper) UpdateUniversalTx(
	ctx context.Context,
	key string,
	updateFn func(*types.UniversalTx) error,
) error {
	// fetch
	utx, found, err := k.GetUniversalTx(ctx, key)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("universal tx with key %s not found", key)
	}

	// apply user-defined mutation
	if err := updateFn(&utx); err != nil {
		return err
	}

	// save back
	return k.UniversalTx.Set(ctx, key, utx)
}

// HasUniversalTx checks if a UniversalTx exists
func (k Keeper) HasUniversalTx(ctx context.Context, key string) (bool, error) {
	return k.UniversalTx.Has(ctx, key)
}

// UpdateUniversalTxStatus sets a new status for the UniversalTx
func (k Keeper) UpdateUniversalTxStatus(ctx context.Context, key string, newStatus types.UniversalTxStatus) error {
	utx, err := k.UniversalTx.Get(ctx, key)
	if err != nil {
		return err
	}

	utx.UniversalStatus = newStatus
	return k.UniversalTx.Set(ctx, key, utx)
}

// GetUniversalTxStatus retrieves the status of a UniversalTx
func (k Keeper) GetUniversalTxStatus(ctx context.Context, key string) (types.UniversalTxStatus, bool, error) {
	utx, found, err := k.GetUniversalTx(ctx, key)
	if err != nil {
		return types.UniversalTxStatus_UNIVERSAL_TX_STATUS_UNSPECIFIED, false, err
	}
	if !found {
		return types.UniversalTxStatus_UNIVERSAL_TX_STATUS_UNSPECIFIED, false, nil
	}
	return utx.UniversalStatus, true, nil
}
