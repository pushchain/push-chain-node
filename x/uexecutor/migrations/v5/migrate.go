package v5

import (
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/pushchain/push-chain-node/x/uexecutor/keeper"
)

// MigrateGasPricesToChainMeta seeds the ChainMetas collection from the legacy
// GasPrices store. This is idempotent — existing ChainMeta entries are not overwritten.
func MigrateGasPricesToChainMeta(ctx sdk.Context, k *keeper.Keeper) error {
	return k.MigrateGasPricesToChainMeta(ctx)
}
