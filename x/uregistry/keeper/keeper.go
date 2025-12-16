package keeper

import (
	"context"
	"errors"
	"strings"

	"github.com/cosmos/cosmos-sdk/codec"

	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	"cosmossdk.io/collections"
	storetypes "cosmossdk.io/core/store"
	"cosmossdk.io/log"
	"github.com/pushchain/push-chain-node/x/uregistry/types"
)

type Keeper struct {
	cdc codec.BinaryCodec

	logger        log.Logger
	schemaBuilder *collections.SchemaBuilder

	// state management
	Params       collections.Item[types.Params]
	ChainConfigs collections.Map[string, types.ChainConfig]
	TokenConfigs collections.Map[string, types.TokenConfig]

	authority string
	evmKeeper types.EVMKeeper
}

// NewKeeper creates a new Keeper instance
func NewKeeper(
	cdc codec.BinaryCodec,
	storeService storetypes.KVStoreService,
	logger log.Logger,
	authority string,
	evmKeeper types.EVMKeeper,
) Keeper {
	logger = logger.With(log.ModuleKey, "x/"+types.ModuleName)

	sb := collections.NewSchemaBuilder(storeService)

	if authority == "" {
		authority = authtypes.NewModuleAddress(govtypes.ModuleName).String()
	}

	k := Keeper{
		cdc:           cdc,
		logger:        logger,
		schemaBuilder: sb,

		Params:       collections.NewItem(sb, types.ParamsKey, types.ParamsName, codec.CollValue[types.Params](cdc)),
		ChainConfigs: collections.NewMap(sb, types.ChainConfigsKey, types.ChainConfigsName, collections.StringKey, codec.CollValue[types.ChainConfig](cdc)),
		TokenConfigs: collections.NewMap(sb, types.TokenConfigsKey, types.TokenConfigsName, collections.StringKey, codec.CollValue[types.TokenConfig](cdc)),

		authority: authority,
		evmKeeper: evmKeeper,
	}

	return k
}

func (k Keeper) Logger() log.Logger {
	return k.logger
}

// InitGenesis initializes the module's state from a genesis state.
func (k *Keeper) InitGenesis(ctx context.Context, data *types.GenesisState) error {

	if err := data.Params.Validate(); err != nil {
		return err
	}

	// deploy system contracts
	deploySystemContracts(ctx, k.evmKeeper)

	return k.Params.Set(ctx, data.Params)
}

// ExportGenesis exports the module's state to a genesis state.
func (k *Keeper) ExportGenesis(ctx context.Context) *types.GenesisState {
	params, err := k.Params.Get(ctx)
	if err != nil {
		panic(err)
	}

	return &types.GenesisState{
		Params: params,
	}
}

func (k Keeper) GetChainConfig(ctx context.Context, chain string) (types.ChainConfig, error) {
	config, err := k.ChainConfigs.Get(ctx, chain)
	if err != nil {
		return types.ChainConfig{}, err
	}
	return config, nil
}

// IsChainInboundEnabled checks if inbound is enabled for a given chain
func (k Keeper) IsChainInboundEnabled(ctx context.Context, chain string) (bool, error) {
	config, err := k.GetChainConfig(ctx, chain)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			// chain not found
			return false, nil
		}
		return false, err
	}
	return config.Enabled.IsInboundEnabled, nil
}

// IsChainOutboundEnabled checks if outbound is enabled for a given chain
func (k Keeper) IsChainOutboundEnabled(ctx context.Context, chain string) (bool, error) {
	config, err := k.GetChainConfig(ctx, chain)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			// chain not found
			return false, nil
		}
		return false, err
	}
	return config.Enabled.IsOutboundEnabled, nil
}

func (k Keeper) GetTokenConfig(ctx context.Context, chain, address string) (types.TokenConfig, error) {
	storageKey := types.GetTokenConfigsStorageKey(chain, address)
	config, err := k.TokenConfigs.Get(ctx, storageKey)
	if err != nil {
		return types.TokenConfig{}, err
	}
	return config, nil
}

func (k Keeper) SchemaBuilder() *collections.SchemaBuilder {
	return k.schemaBuilder
}

func (k Keeper) GetTokenConfigByPRC20(
	ctx context.Context,
	chain string,
	prc20Addr string,
) (types.TokenConfig, error) {

	prc20Addr = strings.ToLower(strings.TrimSpace(prc20Addr))

	var found *types.TokenConfig

	err := k.TokenConfigs.Walk(ctx, nil, func(
		key string,
		cfg types.TokenConfig,
	) (bool, error) {

		// chain must match
		if cfg.Chain != chain {
			return false, nil
		}

		if cfg.NativeRepresentation == nil {
			return false, nil
		}

		if strings.ToLower(cfg.NativeRepresentation.ContractAddress) == prc20Addr {
			found = &cfg
			return true, nil // stop walk
		}

		return false, nil
	})

	if err != nil {
		return types.TokenConfig{}, err
	}

	if found == nil {
		return types.TokenConfig{}, collections.ErrNotFound
	}

	return *found, nil
}
