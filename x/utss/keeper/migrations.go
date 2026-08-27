package keeper

import (
	"context"

	"github.com/pushchain/push-chain-node/x/utss/types"
)

// MigrateFundMigrationsL1GasFee walks every FundMigration record and sets
// L1GasFee to "0" when unset. Records stored before the l1_gas_fee proto
// field existed decode with an empty string; downstream relayer/universalClient
// code parses this value as a decimal wei amount, so we normalize it here.
func (k Keeper) MigrateFundMigrationsL1GasFee(ctx context.Context) error {
	return k.FundMigrations.Walk(ctx, nil, func(id uint64, m types.FundMigration) (bool, error) {
		if m.L1GasFee != "" {
			return false, nil
		}
		m.L1GasFee = "0"
		if err := k.FundMigrations.Set(ctx, id, m); err != nil {
			return true, err
		}
		return false, nil
	})
}
