package v4

import (
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/pushchain/push-chain-node/x/utss/keeper"
)

// MigrateFundMigrationsL1GasFee backfills the new L1GasFee field on existing
// FundMigration records. Records created before v4 decode with L1GasFee == "",
// which is ambiguous for downstream consumers that parse it as a decimal wei
// amount; this migration normalizes those values to "0".
func MigrateFundMigrationsL1GasFee(ctx sdk.Context, k *keeper.Keeper) error {
	return k.MigrateFundMigrationsL1GasFee(ctx)
}
