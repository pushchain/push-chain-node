package keeper

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/cosmos/cosmos-sdk/codec"

	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	"cosmossdk.io/collections"
	storetypes "cosmossdk.io/core/store"
	"cosmossdk.io/log"
	"cosmossdk.io/orm/model/ormdb"

	apiv1 "github.com/push-protocol/push-chain/api/usvl/v1"
	"github.com/push-protocol/push-chain/x/usvl/types"
)

// ChainRpcEnvPrefix is the prefix used for environment variables containing custom RPCs
const ChainRpcEnvPrefix = "USVL_CHAIN_RPC_"

type Keeper struct {
	cdc codec.BinaryCodec

	logger log.Logger

	// state management
	Schema       collections.Schema
	Params       collections.Item[types.Params]
	ChainConfigs collections.Map[string, string] // chainID -> serialized ChainConfigData
	VerifiedTxs  collections.Map[string, string] // txHash:caipAddress -> serialized VerifiedTransaction
	OrmDB        apiv1.StateStore

	authority string
}

// NewKeeper creates a new Keeper instance
func NewKeeper(
	cdc codec.BinaryCodec,
	storeService storetypes.KVStoreService,
	logger log.Logger,
	authority string,
) Keeper {
	logger = logger.With(log.ModuleKey, "x/"+types.ModuleName)

	sb := collections.NewSchemaBuilder(storeService)

	if authority == "" {
		authority = authtypes.NewModuleAddress(govtypes.ModuleName).String()
	}

	db, err := ormdb.NewModuleDB(&types.ORMModuleSchema, ormdb.ModuleDBOptions{KVStoreService: storeService})
	if err != nil {
		panic(err)
	}

	store, err := apiv1.NewStateStore(db)
	if err != nil {
		panic(err)
	}

	k := Keeper{
		cdc:          cdc,
		logger:       logger,
		Params:       collections.NewItem(sb, types.ParamsKey, "params", codec.CollValue[types.Params](cdc)),
		ChainConfigs: collections.NewMap(sb, types.ChainConfigKey, "chain_configs", collections.StringKey, collections.StringValue),
		VerifiedTxs:  collections.NewMap(sb, types.VerifiedTxKey, "verified_txs", collections.StringKey, collections.StringValue),
		OrmDB:        store,
		authority:    authority,
	}

	schema, err := sb.Build()
	if err != nil {
		panic(err)
	}

	k.Schema = schema

	return k
}

func (k Keeper) Logger() log.Logger {
	return k.logger
}

// GetAuthority returns the x/usvl module's authority.
func (k Keeper) GetAuthority() string {
	return k.authority
}

// serializeChainConfig converts ChainConfigData to JSON string
func (k Keeper) serializeChainConfig(config types.ChainConfigData) (string, error) {
	bz, err := json.Marshal(config)
	if err != nil {
		return "", err
	}
	return string(bz), nil
}

// deserializeChainConfig converts JSON string to ChainConfigData
func (k Keeper) deserializeChainConfig(data string) (types.ChainConfigData, error) {
	var config types.ChainConfigData
	err := json.Unmarshal([]byte(data), &config)
	if err != nil {
		return types.ChainConfigData{}, err
	}
	return config, nil
}

// AddChainConfig adds a new chain configuration or updates an existing one
func (k Keeper) AddChainConfig(ctx context.Context, chainConfig types.ChainConfigData) error {
	if err := chainConfig.Validate(); err != nil {
		return err
	}

	// Serialize the config before storing
	serialized, err := k.serializeChainConfig(chainConfig)
	if err != nil {
		return err
	}

	return k.ChainConfigs.Set(ctx, chainConfig.ChainId, serialized)
}

// DeleteChainConfig removes a chain configuration
func (k Keeper) DeleteChainConfig(ctx context.Context, chainID string) error {
	if has, err := k.ChainConfigs.Has(ctx, chainID); err != nil {
		return err
	} else if !has {
		return fmt.Errorf("chain config for %s does not exist", chainID)
	}

	return k.ChainConfigs.Remove(ctx, chainID)
}

// GetChainConfig retrieves a chain configuration by chain ID
func (k Keeper) GetChainConfig(ctx context.Context, chainID string) (types.ChainConfigData, error) {
	serialized, err := k.ChainConfigs.Get(ctx, chainID)
	if err != nil {
		return types.ChainConfigData{}, err
	}

	return k.deserializeChainConfig(serialized)
}

// GetChainConfigWithRPCOverride retrieves a chain configuration and overrides RPC if environment variable is set
func (k Keeper) GetChainConfigWithRPCOverride(ctx context.Context, chainID string) (types.ChainConfigData, error) {
	config, err := k.GetChainConfig(ctx, chainID)
	if err != nil {
		return types.ChainConfigData{}, err
	}

	// Check for environment variable override for RPC URL
	envVarName := fmt.Sprintf("%s%s", ChainRpcEnvPrefix, strings.ToUpper(strings.Replace(chainID, "-", "_", -1)))
	if customRPC := os.Getenv(envVarName); customRPC != "" {
		k.logger.Info("Using custom RPC from environment", "chain_id", chainID, "env_var", envVarName)
		config.PublicRpcUrl = customRPC
	}

	return config, nil
}

// GetAllChainConfigs retrieves all chain configurations
func (k Keeper) GetAllChainConfigs(ctx context.Context) ([]types.ChainConfigData, error) {
	var configs []types.ChainConfigData

	iter, err := k.ChainConfigs.Iterate(ctx, &collections.Range[string]{})
	if err != nil {
		return nil, err
	}
	defer iter.Close()

	for ; iter.Valid(); iter.Next() {
		key, err := iter.Key()
		if err != nil {
			return nil, err
		}

		serialized, err := k.ChainConfigs.Get(ctx, key)
		if err != nil {
			return nil, err
		}

		config, err := k.deserializeChainConfig(serialized)
		if err != nil {
			return nil, err
		}

		configs = append(configs, config)
	}

	return configs, nil
}

// serializeVerifiedTx converts VerifiedTransaction to JSON string
func (k Keeper) serializeVerifiedTx(tx types.VerifiedTransaction) (string, error) {
	bz, err := json.Marshal(tx)
	if err != nil {
		return "", err
	}
	return string(bz), nil
}

// deserializeVerifiedTx converts JSON string to VerifiedTransaction
func (k Keeper) deserializeVerifiedTx(data string) (types.VerifiedTransaction, error) {
	var tx types.VerifiedTransaction
	err := json.Unmarshal([]byte(data), &tx)
	if err != nil {
		return types.VerifiedTransaction{}, err
	}
	return tx, nil
}

// createTxKey creates a compound key for storing transactions
// Format: txHash:caipAddress
func createTxKey(txHash, caipAddress string) string {
	return fmt.Sprintf("%s:%s", txHash, caipAddress)
}

// CreateTxKey creates a compound key for storing transactions (exported for testing)
// Format: txHash:caipAddress
func CreateTxKey(txHash, caipAddress string) string {
	return createTxKey(txHash, caipAddress)
}

// IsTransactionVerified checks if a transaction has already been verified
func (k Keeper) IsTransactionVerified(ctx context.Context, txHash, caipAddress string) (bool, error) {
	key := createTxKey(txHash, caipAddress)
	return k.VerifiedTxs.Has(ctx, key)
}

// StoreVerifiedTransaction stores a verified transaction in the KV store
func (k Keeper) StoreVerifiedTransaction(ctx context.Context, txHash, caipAddress, chainId string) error {
	key := createTxKey(txHash, caipAddress)

	// Check if the transaction already exists
	exists, err := k.VerifiedTxs.Has(ctx, key)
	if err != nil {
		return fmt.Errorf("failed to check if transaction exists: %w", err)
	}

	if exists {
		return fmt.Errorf("transaction already verified: %s for address %s", txHash, caipAddress)
	}

	// Create a new verified transaction record
	tx := types.VerifiedTransaction{
		TxHash:      txHash,
		ChainId:     chainId,
		CaipAddress: caipAddress,
		VerifiedAt:  time.Now().UTC(),
	}

	// Serialize and store the transaction
	serialized, err := k.serializeVerifiedTx(tx)
	if err != nil {
		return fmt.Errorf("failed to serialize transaction: %w", err)
	}

	return k.VerifiedTxs.Set(ctx, key, serialized)
}

// InitGenesis initializes the module's state from a genesis state.
func (k *Keeper) InitGenesis(ctx context.Context, data *types.GenesisState) error {
	if err := data.Params.Validate(); err != nil {
		return err
	}

	if err := k.Params.Set(ctx, data.Params); err != nil {
		return err
	}

	// Initialize chain configs
	for _, protoConfig := range data.ChainConfigs {
		config := types.ChainConfigDataFromProto(protoConfig)
		if err := k.AddChainConfig(ctx, config); err != nil {
			return err
		}
	}

	return nil
}

// ExportGenesis exports the module's state to a genesis state.
func (k *Keeper) ExportGenesis(ctx context.Context) *types.GenesisState {
	params, err := k.Params.Get(ctx)
	if err != nil {
		panic(err)
	}

	configsData, err := k.GetAllChainConfigs(ctx)
	if err != nil {
		panic(err)
	}

	// Convert internal ChainConfigData to proto ChainConfig
	var configs []types.ChainConfig
	for _, config := range configsData {
		configs = append(configs, config.ToProto())
	}

	return &types.GenesisState{
		Params:       params,
		ChainConfigs: configs,
	}
}
