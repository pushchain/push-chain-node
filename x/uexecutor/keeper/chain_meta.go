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

// chainMetaVoteStalenessSeconds is the maximum age (in seconds) of a stored vote
// that is still eligible to be included in the median calculation.
const chainMetaVoteStalenessSeconds uint64 = 300

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

// VoteChainMeta processes a universal validator's vote on chain metadata (gas price + chain height).
//
// Rules:
//  1. If blockNumber <= entry.LastAppliedChainHeight the tx is rejected — the validator
//     must re-vote with a newer block height.
//  2. Each vote is stamped with the current block time (storedAt) when it is recorded.
//  3. When computing medians, only votes whose storedAt is within the last
//     chainMetaVoteStalenessSeconds seconds are considered.
//  4. Price median and chain-height median are computed independently (upper median = len/2).
//  5. After a successful EVM call, LastAppliedChainHeight is updated.
func (k Keeper) VoteChainMeta(ctx context.Context, universalValidator sdk.ValAddress, observedChainId string, price, blockNumber uint64) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := uint64(sdkCtx.BlockTime().Unix())

	entry, found, err := k.GetChainMeta(ctx, observedChainId)
	if err != nil {
		return sdkerrors.Wrap(err, "failed to fetch chain meta entry")
	}

	if !found {
		// First vote for this chain — no height check needed yet.
		k.Logger().Info("chain meta first vote, initializing entry",
			"chain_id", observedChainId,
			"validator", universalValidator.String(),
			"price", price,
			"block_number", blockNumber,
		)
		priceBig := math.NewUint(price).BigInt()
		chainHeightBig := math.NewUint(blockNumber).BigInt()
		if _, evmErr := k.CallUniversalCoreSetChainMeta(sdkCtx, observedChainId, priceBig, chainHeightBig); evmErr != nil {
			return sdkerrors.Wrap(evmErr, "failed to call EVM setChainMeta")
		}

		newEntry := types.ChainMeta{
			ObservedChainId:        observedChainId,
			Signers:                []string{universalValidator.String()},
			Prices:                 []uint64{price},
			ChainHeights:           []uint64{blockNumber},
			StoredAts:              []uint64{now},
			MedianIndex:            0,
			LastAppliedChainHeight: blockNumber,
		}
		if err := k.SetChainMeta(ctx, observedChainId, newEntry); err != nil {
			return sdkerrors.Wrap(err, "failed to set initial chain meta entry")
		}

		return nil
	}

	// Reject votes whose chain height has already been committed to the contract.
	if blockNumber <= entry.LastAppliedChainHeight {
		k.Logger().Warn("chain meta vote rejected: stale block height",
			"chain_id", observedChainId,
			"validator", universalValidator.String(),
			"vote_height", blockNumber,
			"last_applied_height", entry.LastAppliedChainHeight,
		)
		return fmt.Errorf(
			"vote chain height %d is not greater than last applied chain height %d; re-vote with a newer block",
			blockNumber, entry.LastAppliedChainHeight,
		)
	}

	// Update or insert vote for this validator.
	var updated bool
	for i, s := range entry.Signers {
		if s == universalValidator.String() {
			entry.Prices[i] = price
			entry.ChainHeights[i] = blockNumber
			entry.StoredAts[i] = now
			updated = true
			break
		}
	}

	if !updated {
		entry.Signers = append(entry.Signers, universalValidator.String())
		entry.Prices = append(entry.Prices, price)
		entry.ChainHeights = append(entry.ChainHeights, blockNumber)
		entry.StoredAts = append(entry.StoredAts, now)
	}

	// Build a filtered pool: only votes stored within the staleness window.
	type voteSnapshot struct {
		price       uint64
		chainHeight uint64
	}
	var fresh []voteSnapshot
	for i := range entry.Signers {
		if entry.StoredAts[i] > now {
			continue // clock skew guard — skip future-stamped votes
		}
		age := now - entry.StoredAts[i]
		if age <= chainMetaVoteStalenessSeconds {
			fresh = append(fresh, voteSnapshot{
				price:       entry.Prices[i],
				chainHeight: entry.ChainHeights[i],
			})
		}
	}

	if len(fresh) == 0 {
		k.Logger().Debug("chain meta vote recorded, no fresh votes for EVM update",
			"chain_id", observedChainId,
			"validator", universalValidator.String(),
		)
		// No fresh votes — persist the updated entry but skip EVM call.
		if err := k.SetChainMeta(ctx, observedChainId, entry); err != nil {
			return sdkerrors.Wrap(err, "failed to set updated chain meta entry")
		}
		return nil
	}

	// Compute independent upper medians (len/2) for price and chain height.
	medianPrice := upperMedianUint64(fresh, func(v voteSnapshot) uint64 { return v.price })
	medianChainHeight := upperMedianUint64(fresh, func(v voteSnapshot) uint64 { return v.chainHeight })

	k.Logger().Debug("chain meta medians computed",
		"chain_id", observedChainId,
		"fresh_votes", len(fresh),
		"median_price", medianPrice,
		"median_chain_height", medianChainHeight,
	)

	// Update MedianIndex to reflect the price median position in the full slice
	// (best-effort; used for storage/querying only).
	entry.MedianIndex = uint64(computeMedianIndex(entry.Prices))

	priceBig := math.NewUint(medianPrice).BigInt()
	chainHeightBig := math.NewUint(medianChainHeight).BigInt()
	if _, evmErr := k.CallUniversalCoreSetChainMeta(sdkCtx, observedChainId, priceBig, chainHeightBig); evmErr != nil {
		return sdkerrors.Wrap(evmErr, "failed to call EVM setChainMeta")
	}

	entry.LastAppliedChainHeight = medianChainHeight
	if err := k.SetChainMeta(ctx, observedChainId, entry); err != nil {
		return sdkerrors.Wrap(err, "failed to set updated chain meta entry")
	}

	k.Logger().Info("chain meta updated",
		"chain_id", observedChainId,
		"median_price", medianPrice,
		"median_chain_height", medianChainHeight,
	)

	return nil
}

// upperMedianUint64 sorts the slice by the extracted key and returns the value at index len/2
// (upper median for even-length slices).
func upperMedianUint64[T any](items []T, key func(T) uint64) uint64 {
	type kv struct{ k uint64; v T }
	arr := make([]kv, len(items))
	for i, item := range items {
		arr[i] = kv{k: key(item), v: item}
	}
	sort.SliceStable(arr, func(i, j int) bool { return arr[i].k < arr[j].k })
	return arr[len(arr)/2].k
}

// MigrateGasPricesToChainMeta seeds ChainMetas from existing GasPrices entries.
// Called once during the chain-meta upgrade. Existing gas price data (prices, block_nums, median_index)
// is carried over; StoredAts defaults to zero (treated as stale until validators re-vote).
func (k Keeper) MigrateGasPricesToChainMeta(ctx context.Context) error {
	k.Logger().Info("migrating gas prices to chain metas")
	return k.GasPrices.Walk(ctx, nil, func(chainID string, gp types.GasPrice) (bool, error) {
		// Skip if already migrated
		existing, err := k.ChainMetas.Get(ctx, chainID)
		if err == nil && existing.ObservedChainId != "" {
			k.Logger().Debug("chain meta migration skipped: already exists", "chain_id", chainID)
			return false, nil // already exists, skip
		}

		storedAts := make([]uint64, len(gp.Signers))

		cm := types.ChainMeta{
			ObservedChainId:        gp.ObservedChainId,
			Signers:                gp.Signers,
			Prices:                 gp.Prices,
			ChainHeights:           gp.BlockNums,
			StoredAts:              storedAts,
			MedianIndex:            gp.MedianIndex,
			LastAppliedChainHeight: 0,
		}

		if err := k.ChainMetas.Set(ctx, chainID, cm); err != nil {
			return true, err
		}

		k.Logger().Info("chain meta migrated from gas price", "chain_id", chainID, "signer_count", len(gp.Signers))
		return false, nil
	})
}
