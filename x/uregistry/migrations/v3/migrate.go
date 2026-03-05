package v3

import (
	"cosmossdk.io/collections"
	"cosmossdk.io/log"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/pushchain/push-chain-node/x/uregistry/keeper"
	uregistrytypes "github.com/pushchain/push-chain-node/x/uregistry/types"
)

// MigrateChainConfigs initializes the vault_methods field
// (introduced in v3) to its zero-value default for all existing chain configs.
// Since protobuf already decodes missing fields as zero values, this migration
// is a no-op in terms of data transformation but is registered to satisfy the
// module consensus version bump from 2 → 3.
func MigrateChainConfigs(ctx sdk.Context, k *keeper.Keeper, cdc codec.BinaryCodec, logger log.Logger) error {
	logger.Info("Starting ChainConfig migration v3: adding vault_methods field")

	sb := k.SchemaBuilder()

	oldMap := collections.NewMap(
		sb,
		uregistrytypes.ChainConfigsKey,
		uregistrytypes.ChainConfigsName,
		collections.StringKey,
		codec.CollValue[uregistrytypes.ChainConfig](cdc),
	)

	iter, err := oldMap.Iterate(ctx, nil)
	if err != nil {
		return err
	}
	defer iter.Close()

	for ; iter.Valid(); iter.Next() {
		chainID, err := iter.Key()
		if err != nil {
			return err
		}

		cfg, err := iter.Value()
		if err != nil {
			return err
		}

		// vault_methods defaults to empty — no transformation needed.
		// Re-save to ensure the record is encoded with the current schema.
		if err := k.ChainConfigs.Set(ctx, chainID, cfg); err != nil {
			return err
		}

		logger.Info("Migrated ChainConfig v3", "chain_id", chainID)
	}

	logger.Info("Completed ChainConfig migration v3 successfully")
	return nil
}
