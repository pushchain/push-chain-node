package v2

import (
	"time"

	"cosmossdk.io/collections"
	"cosmossdk.io/log"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/pushchain/push-chain-node/x/uregistry/keeper"
	uregistrytypes "github.com/pushchain/push-chain-node/x/uregistry/types"
)

// MigrateChainConfigs sets a default GasOracleFetchInterval for all existing chain configs.
func MigrateChainConfigs(ctx sdk.Context, k *keeper.Keeper, cdc codec.BinaryCodec, logger log.Logger) error {
	logger.Info("Starting ChainConfig migration: adding gas_oracle_fetch_interval")

	sb := k.SchemaBuilder()

	// Old and new share same protobuf key and name, only value structure changed.
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

		// If the duration field is nil or zero, set a sensible default.
		if cfg.GasOracleFetchInterval == 0 {
			cfg.GasOracleFetchInterval = 30 * time.Second
		}

		if err := k.ChainConfigs.Set(ctx, chainID, cfg); err != nil {
			return err
		}

		logger.Info("Migrated ChainConfig", "chain_id", chainID, "fetch_interval", cfg.GasOracleFetchInterval)
	}

	logger.Info("Completed ChainConfig migration successfully")
	return nil
}
