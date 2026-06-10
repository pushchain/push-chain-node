package v7

import (
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/pushchain/push-chain-node/x/uexecutor/keeper"
	"github.com/pushchain/push-chain-node/x/uexecutor/types"
)

// MigratePendingInbounds reshapes the legacy PendingInbounds collection
// (F-2026-16642). Before this version it was a KeySet[string] at prefix 2 —
// a bare set of UTX keys. It is now a Map[string]PendingInboundEntry at the
// same prefix, carrying the per-variant ballot audit trail.
//
// Legacy KeySet rows decode through the new Map codec as an empty
// PendingInboundEntry (the key survives, the value is empty). For each such row
// we rewrite a bare entry that carries its own UtxKey and the upgrade height,
// so it remains a valid pending marker. The variant trail starts empty and is
// refilled by RecordInboundVote as validators vote after the upgrade.
//
// NOTE: an inbound whose UTX key changes under the new tx-hash canonicalization
// (Solana base58 hashes) will re-observe under a fresh key; the bare marker
// here is then swept by the ballot-expiry path. EVM UTX keys are unchanged.
func MigratePendingInbounds(ctx sdk.Context, k *keeper.Keeper) error {
	logger := ctx.Logger()
	logger.Info("🔧 uexecutor v6 → v7: reshaping PendingInbounds KeySet → Map")

	// Collect first; mutating the Map while walking it invalidates the iterator.
	var legacyKeys []string
	err := k.PendingInbounds.Walk(ctx, nil, func(key string, entry types.PendingInboundEntry) (bool, error) {
		// A legacy KeySet row decodes to an empty value (UtxKey == ""); an
		// already-reshaped row carries its UtxKey. Only reshape the former.
		if entry.UtxKey == "" {
			legacyKeys = append(legacyKeys, key)
		}
		return false, nil
	})
	if err != nil {
		return err
	}

	height := uint64(ctx.BlockHeight())
	for _, key := range legacyKeys {
		entry := types.PendingInboundEntry{
			UtxKey:          key,
			Variants:        nil,
			CreatedAtHeight: height,
		}
		if err := k.PendingInbounds.Set(ctx, key, entry); err != nil {
			return err
		}
	}

	logger.Info("✅ uexecutor v6 → v7: pending-inbound reshape complete", "reshaped", len(legacyKeys))
	return nil
}
