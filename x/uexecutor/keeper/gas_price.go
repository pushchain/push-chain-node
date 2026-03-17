package keeper

import (
	"context"
	"errors"
	"fmt"

	"sort"

	"cosmossdk.io/collections"
	sdkerrors "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/pushchain/push-chain-node/x/uexecutor/types"
)

func (k Keeper) GetGasPrice(ctx context.Context, chainID string) (types.GasPrice, bool, error) {
	gp, err := k.GasPrices.Get(ctx, chainID)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return types.GasPrice{}, false, nil
		}
		return types.GasPrice{}, false, err
	}
	return gp, true, nil
}

func (k Keeper) SetGasPrice(ctx context.Context, chainID string, gasPrice types.GasPrice) error {
	return k.GasPrices.Set(ctx, chainID, gasPrice)
}

func (k Keeper) VoteGasPrice(ctx context.Context, universalValidator sdk.ValAddress, observedChainId string, price, blockNumber uint64) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	gasPriceEntry, found, err := k.GetGasPrice(ctx, observedChainId)
	if err != nil {
		return sdkerrors.Wrap(err, "failed to fetch gas price entry")
	}

	if !found {
		// First vote for this chain
		newEntry := types.GasPrice{
			ObservedChainId: observedChainId,
			Signers:         []string{universalValidator.String()},
			Prices:          []uint64{price},
			BlockNums:       []uint64{blockNumber},
			MedianIndex:     0, // Only one value initially
		}

		if err := k.SetGasPrice(ctx, observedChainId, newEntry); err != nil {
			return sdkerrors.Wrap(err, "failed to set initial gas price entry")
		}

		// EVM call
		gasPriceBigInt := math.NewUint(price).BigInt()
		if _, err := k.CallUniversalCoreSetGasPrice(sdkCtx, observedChainId, gasPriceBigInt); err != nil {
			return sdkerrors.Wrap(err, "failed to call EVM setGasPrice")
		}

		return nil
	}

	// Update Entry
	var updated bool
	for i, s := range gasPriceEntry.Signers {
		if s == universalValidator.String() {
			gasPriceEntry.Prices[i] = price
			gasPriceEntry.BlockNums[i] = blockNumber
			updated = true
			break
		}
	}

	if !updated {
		gasPriceEntry.Signers = append(gasPriceEntry.Signers, universalValidator.String())
		gasPriceEntry.Prices = append(gasPriceEntry.Prices, price)
		gasPriceEntry.BlockNums = append(gasPriceEntry.BlockNums, blockNumber)
	}

	// Recompute Median
	medianIdx := computeMedianIndex(gasPriceEntry.Prices)
	gasPriceEntry.MedianIndex = uint64(medianIdx)

	// Call EVM to set gas price
	if err := k.SetGasPrice(ctx, observedChainId, gasPriceEntry); err != nil {
		return sdkerrors.Wrap(err, "failed to set updated gas price entry")
	}

	medianPrice := math.NewUint(gasPriceEntry.Prices[medianIdx]).BigInt()
	if receipt, err := k.CallUniversalCoreSetGasPrice(sdkCtx, observedChainId, medianPrice); err != nil {
		fmt.Println(receipt)
		return sdkerrors.Wrap(err, "failed to call EVM setGasPrice")
	}

	return nil
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

// computeMedianIndexFiltered returns the original index (into prices/observedAts)
// of the validator chosen as the representative for VoteChainMeta. The selection
// is a two-step process:
//
//  1. Compute the median of observedAt timestamps across all validators.
//  2. Exclude validators whose observedAt deviates from that median by more than
//     ObservedAtStalenessThresholdSeconds (they are considered lagging / stale).
//     Using wall-clock seconds is chain-agnostic: it works identically for Solana
//     (0.4 s/block) and Bitcoin (600 s/block) without per-chain configuration.
//  3. Among the remaining validators compute the median of prices and return that
//     validator's original index.
//
// The returned index is valid for all three parallel slices (prices, chainHeights,
// observedAts), so the caller gets a coherent (price, height, timestamp) tuple
// from a single validator who is both current and representative on price.
//
// If – after filtering – no candidates remain (should not happen with a sensible
// threshold), the function falls back to computeMedianIndex on the full price slice.
func computeMedianIndexFiltered(prices, observedAts []uint64) int {
	// Step 1: median observedAt timestamp across all validators
	medianObservedAtIdx := computeMedianIndex(observedAts)
	medianObservedAt := observedAts[medianObservedAtIdx]

	// Step 2: keep only validators within the staleness threshold (in seconds)
	type candidate struct {
		originalIdx int
		price       uint64
	}
	var candidates []candidate
	for i, ts := range observedAts {
		var diff uint64
		if ts >= medianObservedAt {
			diff = ts - medianObservedAt
		} else {
			diff = medianObservedAt - ts
		}
		if diff <= types.ObservedAtStalenessThresholdSeconds {
			candidates = append(candidates, candidate{i, prices[i]})
		}
	}

	// Fallback: threshold filtered everyone out (should not happen)
	if len(candidates) == 0 {
		return computeMedianIndex(prices)
	}

	// Step 3: median of prices among current validators
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].price < candidates[j].price
	})
	return candidates[len(candidates)/2].originalIdx
}
