package keeper

import (
	"context"
	"errors"

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
	TssKeyHistory collections.Map[string, types.TssKey] // map of key_id -> TssKey

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

	if err := k.Params.Set(ctx, data.Params); err != nil {
		return err
	}

	// Restore CurrentTssProcess (optional)
	if data.CurrentTssProcess != nil {
		if err := k.CurrentTssProcess.Set(ctx, *data.CurrentTssProcess); err != nil {
			return err
		}
	}

	// Restore ProcessHistory
	for _, entry := range data.ProcessHistory {
		if err := k.ProcessHistory.Set(ctx, entry.Key, entry.Value); err != nil {
			return err
		}
	}

	// Restore CurrentTssKey (optional)
	if data.CurrentTssKey != nil {
		if err := k.CurrentTssKey.Set(ctx, *data.CurrentTssKey); err != nil {
			return err
		}
	}

	// Restore TssKeyHistory
	for _, entry := range data.TssKeyHistory {
		if err := k.TssKeyHistory.Set(ctx, entry.Key, entry.Value); err != nil {
			return err
		}
	}

	// Restore NextProcessId
	if data.NextProcessId > 0 {
		if err := k.NextProcessId.Set(ctx, data.NextProcessId); err != nil {
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

	// Export CurrentTssProcess (optional, may not exist)
	var currentTssProcess *types.TssKeyProcess
	process, err := k.CurrentTssProcess.Get(ctx)
	if err == nil {
		currentTssProcess = &process
	} else if !errors.Is(err, collections.ErrNotFound) {
		panic(err)
	}

	// Export ProcessHistory
	var processHistory []types.TssKeyProcessEntry
	err = k.ProcessHistory.Walk(ctx, nil, func(key uint64, value types.TssKeyProcess) (bool, error) {
		processHistory = append(processHistory, types.TssKeyProcessEntry{Key: key, Value: value})
		return false, nil
	})
	if err != nil {
		panic(err)
	}

	// Export CurrentTssKey (optional)
	var currentTssKey *types.TssKey
	tssKey, err := k.CurrentTssKey.Get(ctx)
	if err == nil {
		currentTssKey = &tssKey
	} else if !errors.Is(err, collections.ErrNotFound) {
		panic(err)
	}

	// Export TssKeyHistory
	var tssKeyHistory []types.TssKeyEntry
	err = k.TssKeyHistory.Walk(ctx, nil, func(key string, value types.TssKey) (bool, error) {
		tssKeyHistory = append(tssKeyHistory, types.TssKeyEntry{Key: key, Value: value})
		return false, nil
	})
	if err != nil {
		panic(err)
	}

	// Export NextProcessId
	nextProcessId, err := k.NextProcessId.Peek(ctx)
	if err != nil {
		panic(err)
	}

	return &types.GenesisState{
		Params:            params,
		CurrentTssProcess: currentTssProcess,
		ProcessHistory:    processHistory,
		CurrentTssKey:     currentTssKey,
		TssKeyHistory:     tssKeyHistory,
		NextProcessId:     nextProcessId,
	}
}

func (k Keeper) SchemaBuilder() *collections.SchemaBuilder {
	return k.schemaBuilder
}

func (k Keeper) GetUValidatorKeeper() types.UValidatorKeeper {
	return k.uvalidatorKeeper
}
