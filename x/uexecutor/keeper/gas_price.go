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

func (k Keeper) VoteGasPrice(ctx context.Context, universalValidator sdk.ValAddress, chainId string, price, blockNumber uint64) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	gasPriceEntry, found, err := k.GetGasPrice(ctx, chainId)
	if err != nil {
		return sdkerrors.Wrap(err, "failed to fetch gas price entry")
	}

	if !found {
		// First vote for this chain
		newEntry := types.GasPrice{
			ChainId:     chainId,
			Signers:     []string{universalValidator.String()},
			Prices:      []uint64{price},
			BlockNums:   []uint64{blockNumber},
			MedianIndex: 0, // Only one value initially
		}

		if err := k.SetGasPrice(ctx, chainId, newEntry); err != nil {
			return sdkerrors.Wrap(err, "failed to set initial gas price entry")
		}

		// EVM call
		gasPriceBigInt := math.NewUint(price).BigInt()
		if _, err := k.CallUniversalCoreSetGasPrice(sdkCtx, chainId, gasPriceBigInt); err != nil {
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
	if err := k.SetGasPrice(ctx, chainId, gasPriceEntry); err != nil {
		return sdkerrors.Wrap(err, "failed to set updated gas price entry")
	}

	medianPrice := math.NewUint(gasPriceEntry.Prices[medianIdx]).BigInt()
	if receipt, err := k.CallUniversalCoreSetGasPrice(sdkCtx, chainId, medianPrice); err != nil {
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
