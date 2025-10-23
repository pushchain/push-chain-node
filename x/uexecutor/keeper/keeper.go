package keeper

import (
	"context"
	"errors"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"

	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	"cosmossdk.io/collections"
	storetypes "cosmossdk.io/core/store"
	"cosmossdk.io/log"

	"github.com/pushchain/push-chain-node/x/uexecutor/types"
)

type Keeper struct {
	cdc codec.BinaryCodec

	logger        log.Logger
	schemaBuilder *collections.SchemaBuilder

	// state management
	storeService      storetypes.KVStoreService
	Params            collections.Item[types.Params]
	authority         string
	evmKeeper         types.EVMKeeper
	feemarketKeeper   types.FeeMarketKeeper
	bankKeeper        types.BankKeeper
	accountKeeper     types.AccountKeeper
	uregistryKeeper   types.UregistryKeeper
	utxverifierKeeper types.UtxverifierKeeper
	uvalidatorKeeper  types.UValidatorKeeper

	// Inbound trackers
	PendingInbounds collections.KeySet[string]

	// UniversalTx collection
	UniversalTx collections.Map[string, types.UniversalTx]

	// Module account manual nonce
	ModuleAccountNonce collections.Item[uint64]

	// GasPrices collection stores aggregated gas price data for each chain
	GasPrices collections.Map[string, types.GasPrice]
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
	uregistryKeeper types.UregistryKeeper,
	utxverifierKeeper types.UtxverifierKeeper,
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
		storeService:  storeService,
		Params:        collections.NewItem(sb, types.ParamsKey, types.ParamsName, codec.CollValue[types.Params](cdc)),

		authority:         authority,
		evmKeeper:         evmKeeper,
		feemarketKeeper:   feemarketKeeper,
		bankKeeper:        bankKeeper,
		accountKeeper:     accountKeeper,
		uregistryKeeper:   uregistryKeeper,
		utxverifierKeeper: utxverifierKeeper,
		uvalidatorKeeper:  uvalidatorKeeper,

		PendingInbounds: collections.NewKeySet(
			sb,
			types.InboundsKey,
			types.InboundsName,
			collections.StringKey,
		),

		UniversalTx: collections.NewMap(
			sb,
			types.UniversalTxKey,
			types.UniversalTxName,
			collections.StringKey,
			codec.CollValue[types.UniversalTx](cdc),
		),

		ModuleAccountNonce: collections.NewItem(
			sb,
			types.ModuleAccountNonceKey,
			types.ModuleAccountNonceName,
			collections.Uint64Value,
		),

		GasPrices: collections.NewMap(
			sb,
			types.GasPricesKey,
			types.GasPricesName,
			collections.StringKey,
			codec.CollValue[types.GasPrice](cdc),
		),
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

func (k *Keeper) GetUeModuleAddress(ctx context.Context) (common.Address, string) {
	ueModuleAcc := k.accountKeeper.GetModuleAccount(ctx, types.ModuleName) // "ue"
	ueModuleAddr := ueModuleAcc.GetAddress()
	var ethSenderUEAddr common.Address
	copy(ethSenderUEAddr[:], ueModuleAddr.Bytes())

	return ethSenderUEAddr, ethSenderUEAddr.Hex()
}

func (k Keeper) SchemaBuilder() *collections.SchemaBuilder {
	return k.schemaBuilder
}

// GetModuleAccountNonce returns the current module account nonce.
// If not set yet, it safely defaults to 0.
func (k Keeper) GetModuleAccountNonce(ctx sdk.Context) (uint64, error) {
	nonce, err := k.ModuleAccountNonce.Get(ctx)
	if err != nil {
		// If the key is missing, return 0 instead of error
		if errors.Is(err, collections.ErrNotFound) {
			return 0, nil
		}
		return 0, err
	}
	return nonce, nil
}

// IncrementModuleAccountNonce increases the nonce by 1 and stores it back.
func (k Keeper) IncrementModuleAccountNonce(ctx sdk.Context) (uint64, error) {
	nonce, err := k.GetModuleAccountNonce(ctx)
	if err != nil {
		return 0, err
	}
	newNonce := nonce + 1
	if err := k.ModuleAccountNonce.Set(ctx, newNonce); err != nil {
		return 0, err
	}
	return newNonce, nil
}

// SetModuleAccountNonce allows explicitly setting the nonce (optional, for migration or testing).
func (k Keeper) SetModuleAccountNonce(ctx sdk.Context, nonce uint64) error {
	return k.ModuleAccountNonce.Set(ctx, nonce)
}
