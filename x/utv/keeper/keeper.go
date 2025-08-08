package keeper

import (
	"context"
	"errors"
	"fmt"

	"github.com/cosmos/cosmos-sdk/codec"

	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	"cosmossdk.io/collections"
	storetypes "cosmossdk.io/core/store"
	"cosmossdk.io/log"
	"github.com/pushchain/push-chain-node/x/utv/types"
)

type Keeper struct {
	cdc codec.BinaryCodec

	logger        log.Logger
	schemaBuilder *collections.SchemaBuilder

	// state management
	Params             collections.Item[types.Params]
	VerifiedInboundTxs collections.Map[string, types.VerifiedTxMetadata]

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
		cdc:           cdc,
		logger:        logger,
		schemaBuilder: sb,

		Params: collections.NewItem(sb, types.ParamsKey, types.ParamsName, codec.CollValue[types.Params](cdc)),
		VerifiedInboundTxs: collections.NewMap(
			sb,
			types.VerifiedInboundTxsKeyPrefix,
			types.VerifiedInboundTxsName,
			collections.StringKey,
			codec.CollValue[types.VerifiedTxMetadata](cdc),
		),

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

func (k *Keeper) StoreVerifiedInboundTx(ctx context.Context, chain, txHash string, verifiedTxMetadata types.VerifiedTxMetadata) error {
	if chain == "" || txHash == "" {
		return fmt.Errorf("chain, and tx_hash are required")
	}

	storageKey := types.GetVerifiedInboundTxStorageKey(chain, txHash)

	return k.VerifiedInboundTxs.Set(ctx, storageKey, verifiedTxMetadata)
}

func (k *Keeper) GetVerifiedInboundTxMetadata(ctx context.Context, chain, txHash string) (*types.VerifiedTxMetadata, bool, error) {
	storageKey := types.GetVerifiedInboundTxStorageKey(chain, txHash)

	data, err := k.VerifiedInboundTxs.Get(ctx, storageKey)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			// Not found, but not an actual error
			return nil, false, nil
		}
		// Actual decoding or other storage error
		return nil, false, err
	}

	return &data, true, nil
}

func (k Keeper) SchemaBuilder() *collections.SchemaBuilder {
	return k.schemaBuilder
}

func (k Keeper) GetUEKeeper() types.UeKeeper {
	return k.ueKeeper
}
