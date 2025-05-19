package keeper

import (
	"context"
	"fmt"
	"math/big"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	pchaintypes "github.com/rollchains/pchain/types"
	"github.com/rollchains/pchain/x/crosschain/types"
)

func (k Keeper) DeductAndBurnFees(ctx context.Context, from sdk.AccAddress, gasUsed *big.Int) error {
	amt := sdkmath.NewIntFromBigInt(gasUsed) // optionally multiply with gasPrice
	// TODO: Calculate fees based on base fee & priority fee
	coin := sdk.NewCoin(pchaintypes.BaseDenom, amt)

	err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, from, types.ModuleName, sdk.NewCoins(coin))
	if err != nil {
		return err
	}

	return k.bankKeeper.BurnCoins(ctx, types.ModuleName, sdk.NewCoins(coin))
}

// CalculateGasCost calculates the gas cost based on the base fee, max fee per gas, max priority fee per gas, and gas used.
// Effective Gas Price = min(max_fee_per_gas, base_fee + max_priority_fee_per_gas)
// Total Fee = gas_used × Effective Gas Price
func (k Keeper) CalculateGasCost(
	baseFee sdkmath.LegacyDec,
	maxFeePerGas *big.Int,
	maxPriorityFeePerGas *big.Int,
	gasUsed uint64,
) (*big.Int, error) {
	baseFeeBig := baseFee.BigInt()

	// Check if maxFeePerGas is less than baseFee
	if maxFeePerGas.Cmp(baseFeeBig) < 0 {
		// If maxFeePerGas is less than baseFee, return an error
		return nil, fmt.Errorf("maxFeePerGas (%s) cannot be less than baseFee (%s)", maxFeePerGas, baseFeeBig)
	}

	// baseFee + maxPriorityFeePerGas
	tipPlusBase := new(big.Int).Add(baseFeeBig, maxPriorityFeePerGas)

	// min(maxFeePerGas, baseFee + maxPriorityFeePerGas)
	effectiveGasPrice := new(big.Int).Set(maxFeePerGas)
	if tipPlusBase.Cmp(maxFeePerGas) == -1 {
		effectiveGasPrice = tipPlusBase
	}

	// gasCost = effectiveGasPrice * gasUsed
	gasUsedBig := new(big.Int).SetUint64(gasUsed)
	gasCost := new(big.Int).Mul(effectiveGasPrice, gasUsedBig)

	// TODO: ADJUST GAS CALCULATION ACC TO DECIMALS
	return gasCost, nil
}
