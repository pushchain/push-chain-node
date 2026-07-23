package v4

import (
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/pushchain/push-chain-node/x/uregistry/keeper"
	"github.com/pushchain/push-chain-node/x/uregistry/types"
)

// MigrateTokenConfigs canonicalizes token storage keys and backfills the PRC20
// reverse index (F-2026-17022). Before this version tokens were keyed by the
// raw address (case-sensitive) and there was no PRC20 index. The keeper now
// keys every row by the canonical address (EIP-55 for EVM) and maintains a
// PRC20 → storage-key index for O(1) GetTokenConfigByPRC20.
//
// This migration re-keys any row whose stored key differs from its canonical
// key, then (re)builds the PRC20 index over every row. Both steps are
// idempotent and safe to run on already-canonical / empty state.
func MigrateTokenConfigs(ctx sdk.Context, k *keeper.Keeper) error {
	logger := ctx.Logger()
	logger.Info("🔧 uregistry v3 → v4: canonicalizing token keys + building PRC20 index")

	type row struct {
		oldKey string
		cfg    types.TokenConfig
	}

	// Collect first; mutating the IndexedMap while walking it invalidates the
	// iterator.
	var rows []row
	err := k.TokenConfigs.Walk(ctx, nil, func(key string, cfg types.TokenConfig) (bool, error) {
		rows = append(rows, row{oldKey: key, cfg: cfg})
		return false, nil
	})
	if err != nil {
		return err
	}

	rekeyed := 0
	for _, r := range rows {
		canonKey := types.GetTokenConfigsStorageKey(r.cfg.Chain, r.cfg.Address)
		if canonKey == r.oldKey {
			continue
		}
		if err := k.TokenConfigs.Remove(ctx, r.oldKey); err != nil {
			return err
		}
		if err := k.TokenConfigs.Set(ctx, canonKey, r.cfg); err != nil {
			return err
		}
		rekeyed++
	}

	// Backfill the PRC20 index for every row (rows that were never re-keyed
	// still have no index entry from the pre-upgrade state).
	if err := k.RebuildPRC20Index(ctx); err != nil {
		return err
	}

	logger.Info("✅ uregistry v3 → v4: token key migration complete", "rows", len(rows), "rekeyed", rekeyed)
	return nil
}
