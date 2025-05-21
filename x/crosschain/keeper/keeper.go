package keeper

import (
	"context"

	"github.com/cosmos/cosmos-sdk/codec"

	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	"cosmossdk.io/collections"
	storetypes "cosmossdk.io/core/store"
	"cosmossdk.io/log"

	"github.com/push-protocol/push-chain/x/crosschain/types"
)

type Keeper struct {
	cdc codec.BinaryCodec

	logger log.Logger

	// state management
	storeService storetypes.KVStoreService
	Params       collections.Item[types.Params]
	AdminParams  collections.Item[types.AdminParams]

	authority       string
	evmKeeper       types.EVMKeeper
	feemarketKeeper types.FeeMarketKeeper
	bankKeeper      types.BankKeeper
	usvlKeeper      types.USVLKeeper
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
	usvlKeeper types.USVLKeeper,
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
		AdminParams:  collections.NewItem(sb, types.AdminParamsKey, types.AdminParamsName, codec.CollValue[types.AdminParams](cdc)),

		authority:       authority,
		evmKeeper:       evmKeeper,
		feemarketKeeper: feemarketKeeper,
		bankKeeper:      bankKeeper,
		usvlKeeper:      usvlKeeper,
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
