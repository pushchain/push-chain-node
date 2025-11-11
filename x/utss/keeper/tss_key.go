package keeper

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/collections"

	"github.com/pushchain/push-chain-node/x/utss/types"
)

// SetCurrentTssKey stores the finalized active TSS key.
func (k Keeper) SetCurrentTssKey(ctx context.Context, key types.TssKey) error {
	if err := key.ValidateBasic(); err != nil {
		return fmt.Errorf("invalid tss key: %w", err)
	}

	if err := k.CurrentTssKey.Set(ctx, key); err != nil {
		return fmt.Errorf("failed to set current tss key: %w", err)
	}

	// Also store in TssKeyHistory for reference
	if err := k.TssKeyHistory.Set(ctx, key.KeyId, key); err != nil {
		return fmt.Errorf("failed to record tss key history: %w", err)
	}

	k.Logger().Info("New TSS key finalized", "key_id", key.KeyId, "pubkey", key.TssPubkey)
	return nil
}

// GetCurrentTssKey fetches the currently active finalized key.
func (k Keeper) GetCurrentTssKey(ctx context.Context) (types.TssKey, bool, error) {
	key, err := k.CurrentTssKey.Get(ctx)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return types.TssKey{}, false, nil
		}
		return types.TssKey{}, false, err
	}
	return key, true, nil
}

// GetTssKeyByID retrieves a specific key from history using key_id.
func (k Keeper) GetTssKeyByID(ctx context.Context, keyID string) (types.TssKey, bool, error) {
	key, err := k.TssKeyHistory.Get(ctx, keyID)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return types.TssKey{}, false, nil
		}
		return types.TssKey{}, false, err
	}
	return key, true, nil
}
