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

// VoteChainMeta processes a universal validator's vote on chain metadata (gas price + chain height + observed timestamp).
// It accumulates votes, computes the median price, and calls setChainMeta on the UniversalCore contract.
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

		// EVM call with single price
		priceBig := math.NewUint(price).BigInt()
		chainHeightBig := math.NewUint(blockNumber).BigInt()
		observedAtBig := math.NewUint(observedAt).BigInt()
		if _, evmErr := k.CallUniversalCoreSetChainMeta(sdkCtx, observedChainId, priceBig, chainHeightBig, observedAtBig); evmErr != nil {
			// Non-fatal: log the error. The EVM call may fail if the contract hasn't been upgraded yet
			// (old bytecode only has setGasPrice). State is still persisted.
			sdkCtx.Logger().Error("VoteChainMeta: EVM setChainMeta call failed", "chain", observedChainId, "error", evmErr)
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

	// Recompute median on price
	medianIdx := computeMedianIndex(entry.Prices)
	entry.MedianIndex = uint64(medianIdx)

	if err := k.SetChainMeta(ctx, observedChainId, entry); err != nil {
		return sdkerrors.Wrap(err, "failed to set updated chain meta entry")
	}

	medianPrice := math.NewUint(entry.Prices[medianIdx]).BigInt()
	medianChainHeight := math.NewUint(entry.ChainHeights[medianIdx]).BigInt()
	medianObservedAt := math.NewUint(entry.ObservedAts[medianIdx]).BigInt()
	if _, evmErr := k.CallUniversalCoreSetChainMeta(sdkCtx, observedChainId, medianPrice, medianChainHeight, medianObservedAt); evmErr != nil {
		// Non-fatal: log. Same forward-compat reason as above.
		sdkCtx.Logger().Error("VoteChainMeta: EVM setChainMeta call failed", "chain", observedChainId, "error", evmErr)
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
