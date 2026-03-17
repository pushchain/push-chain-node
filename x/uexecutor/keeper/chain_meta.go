package keeper

import (
	"context"
	"errors"

	"cosmossdk.io/collections"
	sdkerrors "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/pushchain/push-chain-node/x/uexecutor/types"
)

func (k Keeper) GetChainMeta(ctx context.Context, chainID string) (types.ChainMeta, bool, error) {
	cm, err := k.ChainMetas.Get(ctx, chainID)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return types.ChainMeta{}, false, nil
		}
		return types.ChainMeta{}, false, err
	}
	return cm, true, nil
}

func (k Keeper) SetChainMeta(ctx context.Context, chainID string, chainMeta types.ChainMeta) error {
	return k.ChainMetas.Set(ctx, chainID, chainMeta)
}

// isObservedAtStale returns true if observedAt (Unix seconds) is older than
// ObservedAtStalenessThresholdSeconds relative to the current block time.
// When true the data should not be pushed to the UniversalCore contract.
// Returns false when block time is zero or negative (not yet configured).
func isObservedAtStale(sdkCtx sdk.Context, observedAt uint64) bool {
	blockTimeUnix := sdkCtx.BlockTime().Unix()
	if blockTimeUnix <= 0 {
		// Block time not configured (e.g. genesis or test setup) — skip staleness check.
		return false
	}
	blockTimeSec := uint64(blockTimeUnix)
	return blockTimeSec > observedAt &&
		blockTimeSec-observedAt > types.ObservedAtStalenessThresholdSeconds
}

// VoteChainMeta processes a universal validator's vote on chain metadata (gas price + chain height + observed timestamp).
// It accumulates votes, computes the median price, and calls setChainMeta on the UniversalCore contract.
// If the median observedAt is older than ObservedAtStalenessThresholdSeconds relative to the current
// block time, the EVM contract is NOT updated — all validators are considered stale and retaining
// the last good contract state is preferred over pushing stale data.
func (k Keeper) VoteChainMeta(ctx context.Context, universalValidator sdk.ValAddress, observedChainId string, price, blockNumber, observedAt uint64) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	entry, found, err := k.GetChainMeta(ctx, observedChainId)
	if err != nil {
		return sdkerrors.Wrap(err, "failed to fetch chain meta entry")
	}

	if !found {
		// First vote for this chain
		newEntry := types.ChainMeta{
			ObservedChainId: observedChainId,
			Signers:         []string{universalValidator.String()},
			Prices:          []uint64{price},
			ChainHeights:    []uint64{blockNumber},
			ObservedAts:     []uint64{observedAt},
			MedianIndex:     0,
		}

		if err := k.SetChainMeta(ctx, observedChainId, newEntry); err != nil {
			return sdkerrors.Wrap(err, "failed to set initial chain meta entry")
		}

		if isObservedAtStale(sdkCtx, observedAt) {
			sdkCtx.Logger().Info("VoteChainMeta: skipping EVM update — single vote is stale",
				"chain", observedChainId, "observedAt", observedAt)
			return nil
		}

		priceBig := math.NewUint(price).BigInt()
		chainHeightBig := math.NewUint(blockNumber).BigInt()
		observedAtBig := math.NewUint(observedAt).BigInt()
		if _, evmErr := k.CallUniversalCoreSetChainMeta(sdkCtx, observedChainId, priceBig, chainHeightBig, observedAtBig); evmErr != nil {
			return sdkerrors.Wrap(evmErr, "failed to call EVM setChainMeta")
		}

		return nil
	}

	// Update or insert vote for this validator
	var updated bool
	for i, s := range entry.Signers {
		if s == universalValidator.String() {
			entry.Prices[i] = price
			entry.ChainHeights[i] = blockNumber
			entry.ObservedAts[i] = observedAt
			updated = true
			break
		}
	}

	if !updated {
		entry.Signers = append(entry.Signers, universalValidator.String())
		entry.Prices = append(entry.Prices, price)
		entry.ChainHeights = append(entry.ChainHeights, blockNumber)
		entry.ObservedAts = append(entry.ObservedAts, observedAt)
	}

	// Recompute median: filter out stale validators first, then median on price.
	// Staleness is measured by observedAt (Unix seconds) — chain-agnostic.
	// See computeMedianIndexFiltered for the two-step algorithm.
	medianIdx := computeMedianIndexFiltered(entry.Prices, entry.ObservedAts)
	entry.MedianIndex = uint64(medianIdx)

	if err := k.SetChainMeta(ctx, observedChainId, entry); err != nil {
		return sdkerrors.Wrap(err, "failed to set updated chain meta entry")
	}

	// If the median observedAt is stale relative to the current block time, all
	// validators are considered offline/lagging. Skip the EVM update so the
	// contract retains its last known good value rather than being overwritten
	// with stale data.
	medianObservedAt := entry.ObservedAts[medianIdx]
	if isObservedAtStale(sdkCtx, medianObservedAt) {
		sdkCtx.Logger().Info("VoteChainMeta: skipping EVM update — all validators stale",
			"chain", observedChainId, "medianObservedAt", medianObservedAt)
		return nil
	}

	// Use the full observation tuple from the median-price validator.
	// chainHeight and observedAt are NOT independent medians — they are the
	// co-indexed values from whichever validator submitted the median price.
	medianPrice := math.NewUint(entry.Prices[medianIdx]).BigInt()
	coChainHeight := math.NewUint(entry.ChainHeights[medianIdx]).BigInt()
	coObservedAt := math.NewUint(medianObservedAt).BigInt()
	if _, evmErr := k.CallUniversalCoreSetChainMeta(sdkCtx, observedChainId, medianPrice, coChainHeight, coObservedAt); evmErr != nil {
		return sdkerrors.Wrap(evmErr, "failed to call EVM setChainMeta")
	}

	return nil
}

// MigrateGasPricesToChainMeta seeds ChainMetas from existing GasPrices entries.
// Called once during the chain-meta upgrade. Existing gas price data (prices, block_nums, median_index)
// is carried over; observedAts is set to 0 since the timestamp was not tracked before.
func (k Keeper) MigrateGasPricesToChainMeta(ctx context.Context) error {
	return k.GasPrices.Walk(ctx, nil, func(chainID string, gp types.GasPrice) (bool, error) {
		// Skip if already migrated
		existing, err := k.ChainMetas.Get(ctx, chainID)
		if err == nil && existing.ObservedChainId != "" {
			return false, nil // already exists, skip
		}

		observedAts := make([]uint64, len(gp.Prices))
		// observedAts unknown at migration time — leave as 0

		cm := types.ChainMeta{
			ObservedChainId: gp.ObservedChainId,
			Signers:         gp.Signers,
			Prices:          gp.Prices,
			ChainHeights:    gp.BlockNums,
			ObservedAts:     observedAts,
			MedianIndex:     gp.MedianIndex,
		}

		if err := k.ChainMetas.Set(ctx, chainID, cm); err != nil {
			return true, err
		}

		return false, nil
	})
}
