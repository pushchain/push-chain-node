package keeper

import (
	"context"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	pchaintypes "github.com/rollchains/pchain/types"
	"github.com/rollchains/pchain/x/crosschain/types"
)

func (k Keeper) DeductAndBurnFees(ctx context.Context, from sdk.AccAddress, gasUsed uint64) error {
	amt := sdkmath.NewInt(int64(gasUsed)) // optionally multiply with gasPrice
	// TODO: Calculate fees based on base fee & priority fee
	coin := sdk.NewCoin(pchaintypes.BaseDenom, amt)

	err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, from, types.ModuleName, sdk.NewCoins(coin))
	if err != nil {
		return err
	}

	return k.bankKeeper.BurnCoins(ctx, types.ModuleName, sdk.NewCoins(coin))
}
