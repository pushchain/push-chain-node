package keeper

import (
	"context"
	"sort"

	"github.com/pushchain/push-chain-node/x/uexecutor/types"
)

// SetGasPrice — deprecated. Kept for legacy migration/test compatibility.
func (k Keeper) SetGasPrice(ctx context.Context, chainID string, gasPrice types.GasPrice) error {
	return k.GasPrices.Set(ctx, chainID, gasPrice)
}

// PruneValidatorVotes removes a validator's votes from all ChainMetas entries.
// Called when a validator is removed from the universal validator set to prevent stale votes
// from influencing the median calculations.
func (k Keeper) PruneValidatorVotes(ctx context.Context, validatorAddr string) {
	k.Logger().Debug("pruning validator votes from chain metas", "validator", validatorAddr)

	_ = k.ChainMetas.Walk(ctx, nil, func(chainId string, entry types.ChainMeta) (bool, error) {
		idx := -1
		for i, s := range entry.Signers {
			if s == validatorAddr {
				idx = i
				break
			}
		}
		if idx >= 0 && len(entry.Signers) > 1 {
			entry.Signers = append(entry.Signers[:idx], entry.Signers[idx+1:]...)
			entry.Prices = append(entry.Prices[:idx], entry.Prices[idx+1:]...)
			entry.ChainHeights = append(entry.ChainHeights[:idx], entry.ChainHeights[idx+1:]...)
			entry.StoredAts = append(entry.StoredAts[:idx], entry.StoredAts[idx+1:]...)
			entry.MedianIndex = uint64(computeMedianIndex(entry.Prices))
			_ = k.ChainMetas.Set(ctx, chainId, entry)
			k.Logger().Debug("pruned validator vote from chain meta",
				"validator", validatorAddr,
				"chain_id", chainId,
				"remaining_signers", len(entry.Signers),
			)
		} else if idx >= 0 && len(entry.Signers) == 1 {
			_ = k.ChainMetas.Remove(ctx, chainId)
			k.Logger().Debug("removed chain meta entry after last signer pruned",
				"validator", validatorAddr,
				"chain_id", chainId,
			)
		}
		return false, nil
	})
}

// computeMedianIndex returns index of the median element
func computeMedianIndex(values []uint64) int {
	type idxVal struct {
		Idx int
		Val uint64
	}
	arr := make([]idxVal, len(values))
	for i, v := range values {
		arr[i] = idxVal{Idx: i, Val: v}
	}
	sort.SliceStable(arr, func(i, j int) bool { return arr[i].Val < arr[j].Val })
	return arr[len(arr)/2].Idx
}
