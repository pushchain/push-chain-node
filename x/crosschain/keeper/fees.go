package keeper

import (
	"context"
	"fmt"
	"math/big"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	pchaintypes "github.com/push-protocol/push-chain/types"
	"github.com/push-protocol/push-chain/x/crosschain/types"
)

// DeductAndBurnFees deducts gas fees from the user's smart account and burns them.
// The process happens in two steps:
// 1. Transfer coins from user account to module account
// 2. Burn coins from module account
// Returns error if either transfer or burn fails
func (k Keeper) DeductAndBurnFees(ctx context.Context, from sdk.AccAddress, gasCost *big.Int) error {
	amt := sdkmath.NewIntFromBigInt(gasCost)
	coin := sdk.NewCoin(pchaintypes.BaseDenom, amt)

	err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, from, types.ModuleName, sdk.NewCoins(coin))
	if err != nil {
		return err
	}

	return k.bankKeeper.BurnCoins(ctx, types.ModuleName, sdk.NewCoins(coin))
}

// CalculateGasCost calculates the gas cost based on EIP-1559 fee mechanism:
// 1. Effective Gas Price = min(maxFeePerGas, baseFee + maxPriorityFeePerGas)
// 2. Total Fee = gasUsed × Effective Gas Price
// Parameters:
// - baseFee: current network base fee
// - maxFeePerGas: maximum total fee user is willing to pay
// - maxPriorityFeePerGas: maximum tip to validator
// - gasUsed: amount of gas consumed
func (k Keeper) CalculateGasCost(
	baseFee sdkmath.LegacyDec,
	maxFeePerGas *big.Int,
	maxPriorityFeePerGas *big.Int,
	gasUsed uint64,
) (*big.Int, error) {
	baseFeeBig := baseFee.BigInt()

	// Step 1: Validate maxFeePerGas >= baseFee
	if maxFeePerGas.Cmp(baseFeeBig) < 0 {
		return nil, fmt.Errorf("maxFeePerGas (%s) cannot be less than baseFee (%s)", maxFeePerGas, baseFeeBig)
	}

	// Step 2: Calculate baseFee + maxPriorityFeePerGas (potential effective gas price)
	tipPlusBase := new(big.Int).Add(baseFeeBig, maxPriorityFeePerGas)

	// Step 3: Find effective gas price by taking minimum
	effectiveGasPrice := new(big.Int).Set(maxFeePerGas)
	if tipPlusBase.Cmp(maxFeePerGas) == -1 {
		effectiveGasPrice = tipPlusBase
	}

	// Step 4: Calculate final gas cost: effectiveGasPrice * gasUsed
	gasUsedBig := new(big.Int).SetUint64(gasUsed)
	gasCost := new(big.Int).Mul(effectiveGasPrice, gasUsedBig)

	return gasCost, nil
}
