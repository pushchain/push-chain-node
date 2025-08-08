package keeper

import (
	"context"

	"github.com/cosmos/cosmos-sdk/codec"

	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	"cosmossdk.io/collections"
	storetypes "cosmossdk.io/core/store"
	"cosmossdk.io/log"

	"github.com/pushchain/push-chain-node/x/ue/types"
)

type Keeper struct {
	cdc codec.BinaryCodec

	logger log.Logger

	// state management
	storeService storetypes.KVStoreService
	Params       collections.Item[types.Params]
	ChainConfigs collections.Map[string, types.ChainConfig]

	authority       string
	evmKeeper       types.EVMKeeper
	feemarketKeeper types.FeeMarketKeeper
	bankKeeper      types.BankKeeper
	accountKeeper   types.AccountKeeper
	utvKeeper       types.UtvKeeper
}

// NewKeeper creates a new Keeper instance
func NewKeeper(
	cdc codec.BinaryCodec,
	storeService storetypes.KVStoreService,
	logger log.Logger,
	authority string,
	evmKeeper types.EVMKeeper,
	feemarketKeeper types.FeeMarketKeeper,
	bankKeeper types.BankKeeper,
	accountKeeper types.AccountKeeper,
	utvKeeper types.UtvKeeper,
) Keeper {
	logger = logger.With(log.ModuleKey, "x/"+types.ModuleName)

	sb := collections.NewSchemaBuilder(storeService)

	if authority == "" {
		authority = authtypes.NewModuleAddress(govtypes.ModuleName).String()
	}

	k := Keeper{
		cdc:          cdc,
		logger:       logger,
		storeService: storeService,
		Params:       collections.NewItem(sb, types.ParamsKey, types.ParamsName, codec.CollValue[types.Params](cdc)),
		ChainConfigs: collections.NewMap(sb, types.ChainConfigsKey, types.ChainConfigsName, collections.StringKey, codec.CollValue[types.ChainConfig](cdc)),

		authority:       authority,
		evmKeeper:       evmKeeper,
		feemarketKeeper: feemarketKeeper,
		bankKeeper:      bankKeeper,
		accountKeeper:   accountKeeper,
		utvKeeper:       utvKeeper,
	}

	return k
}

func (k Keeper) Logger() log.Logger {
	return k.logger
}

// InitGenesis initializes the module's state from a genesis state.
func (k *Keeper) InitGenesis(ctx context.Context, data *types.GenesisState) error {

	if err := data.Params.ValidateBasic(); err != nil {
		return err
	}

	// deploy factory proxy at 0xEA address
	deployFactoryEA(ctx, k.evmKeeper)

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

func (k Keeper) IsChainEnabled(ctx context.Context, chain string) (bool, error) {
	enabled, err := k.ChainConfigs.Has(ctx, chain)
	if err != nil {
		return false, err
	}
	return enabled, nil
}
