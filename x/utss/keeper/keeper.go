package keeper

import (
	"context"

	"github.com/cosmos/cosmos-sdk/codec"

	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	"cosmossdk.io/collections"
	storetypes "cosmossdk.io/core/store"
	"cosmossdk.io/log"
	"github.com/pushchain/push-chain-node/x/utss/types"
)

type Keeper struct {
	cdc codec.BinaryCodec

	logger        log.Logger
	schemaBuilder *collections.SchemaBuilder

	// Module State
	Params            collections.Item[types.Params]               // module params
	NextProcessId     collections.Sequence                         // counter for next process id
	CurrentTssProcess collections.Item[types.TssKeyProcess]        // current/active process
	ProcessHistory    collections.Map[uint64, types.TssKeyProcess] // history of past processes

	// TSS Key Storage
	CurrentTssKey collections.Item[types.TssKey]        // currently active finalized key
	TssKeyHistory collections.Map[string, types.TssKey] // map of key_id → TssKey

	// keepers
	uvalidatorKeeper types.UValidatorKeeper

	authority string
}

// NewKeeper creates a new Keeper instance
func NewKeeper(
	cdc codec.BinaryCodec,
	storeService storetypes.KVStoreService,
	logger log.Logger,
	authority string,
	uvalidatorKeeper types.UValidatorKeeper,
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

		Params:            collections.NewItem(sb, types.ParamsKey, types.ParamsName, codec.CollValue[types.Params](cdc)),
		NextProcessId:     collections.NewSequence(sb, types.NextProcessIdKey, "next_process_id"),
		CurrentTssProcess: collections.NewItem(sb, types.CurrentTssProcessKey, "current_tss_process", codec.CollValue[types.TssKeyProcess](cdc)),
		ProcessHistory:    collections.NewMap(sb, types.ProcessHistoryKey, "process_history", collections.Uint64Key, codec.CollValue[types.TssKeyProcess](cdc)),

		// TSS key storage
		CurrentTssKey: collections.NewItem(sb, types.CurrentTssKeyKeyPrefix, "current_tss_key", codec.CollValue[types.TssKey](cdc)),
		TssKeyHistory: collections.NewMap(sb, types.TssKeyHistoryKey, "tss_key_history", collections.StringKey, codec.CollValue[types.TssKey](cdc)),

		authority:        authority,
		uvalidatorKeeper: uvalidatorKeeper,
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

func (k Keeper) SchemaBuilder() *collections.SchemaBuilder {
	return k.schemaBuilder
}

func (k Keeper) GetUValidatorKeeper() types.UValidatorKeeper {
	return k.uvalidatorKeeper
}
