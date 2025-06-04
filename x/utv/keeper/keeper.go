package keeper

import (
	"context"
	"fmt"

	"github.com/cosmos/cosmos-sdk/codec"

	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	"cosmossdk.io/collections"
	storetypes "cosmossdk.io/core/store"
	"cosmossdk.io/log"
	"github.com/rollchains/pchain/x/utv/types"
)

type Keeper struct {
	cdc codec.BinaryCodec

	logger log.Logger

	// state management
	Params      collections.Item[types.Params]
	VerifiedTxs collections.Map[string, bool]

	// keepers
	ueKeeper types.UeKeeper

	authority string
}

// NewKeeper creates a new Keeper instance
func NewKeeper(
	cdc codec.BinaryCodec,
	storeService storetypes.KVStoreService,
	logger log.Logger,
	authority string,
	ueKeeper types.UeKeeper,
) Keeper {
	logger = logger.With(log.ModuleKey, "x/"+types.ModuleName)

	sb := collections.NewSchemaBuilder(storeService)

	if authority == "" {
		authority = authtypes.NewModuleAddress(govtypes.ModuleName).String()
	}

	k := Keeper{
		cdc:    cdc,
		logger: logger,

		Params:      collections.NewItem(sb, types.ParamsKey, types.ParamsName, codec.CollValue[types.Params](cdc)),
		VerifiedTxs: collections.NewMap(sb, types.VerifiedTxsKeyPrefix, types.VerifiedTxsName, collections.StringKey, collections.BoolValue),

		authority: authority,
		ueKeeper:  ueKeeper,
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

func (k *Keeper) storeVerifiedTx(ctx context.Context, chainId, txHash string) error {
	if chainId == "" || txHash == "" {
		return fmt.Errorf("chain_id and tx_hash are required")
	}

	storageKey := types.GetVerifiedTxStorageKey(chainId, txHash)
	return k.VerifiedTxs.Set(ctx, storageKey, true)
}

func (k *Keeper) IsTxHashVerified(ctx context.Context, chainId, txHash string) (bool, error) {
	storageKey := types.GetVerifiedTxStorageKey(chainId, txHash)

	// Check if tx hash exists for passed chainId
	if has, err := k.VerifiedTxs.Has(ctx, storageKey); err != nil {
		return false, err
	} else if !has {
		return false, nil // Not present
	}

	return true, nil // Verified
}
