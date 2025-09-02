package keeper

import (
	"context"
	"errors"

	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/ethereum/go-ethereum/common"

	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	"cosmossdk.io/collections"
	storetypes "cosmossdk.io/core/store"
	"cosmossdk.io/log"
	"github.com/pushchain/push-chain-node/x/uregistry/types"
)

type Keeper struct {
	cdc codec.BinaryCodec

	logger log.Logger

	// state management
<<<<<<< HEAD
<<<<<<<< HEAD:x/uregistry/keeper/keeper.go
=======
>>>>>>> feat/universal-validator
	Params       collections.Item[types.Params]
	ChainConfigs collections.Map[string, types.ChainConfig]
	TokenConfigs collections.Map[string, types.TokenConfig]

	authority string
<<<<<<< HEAD
========
	storeService     storetypes.KVStoreService
	Params           collections.Item[types.Params]
	authority        string
	evmKeeper        types.EVMKeeper
	feemarketKeeper  types.FeeMarketKeeper
	bankKeeper       types.BankKeeper
	accountKeeper    types.AccountKeeper
	uregistryKeeper  types.UregistryKeeper
	utvKeeper        types.UtvKeeper
	uvalidatorKeeper types.UValidatorKeeper

	// Inbound trackers
	PendingInbounds collections.KeySet[string]

	// UniversalTx collection
	UniversalTx collections.Map[string, types.UniversalTx]
>>>>>>>> feat/universal-validator:x/ue/keeper/keeper.go
=======
>>>>>>> feat/universal-validator
}

// NewKeeper creates a new Keeper instance
func NewKeeper(
	cdc codec.BinaryCodec,
	storeService storetypes.KVStoreService,
	logger log.Logger,
	authority string,
<<<<<<< HEAD
<<<<<<<< HEAD:x/uregistry/keeper/keeper.go
========
	evmKeeper types.EVMKeeper,
	feemarketKeeper types.FeeMarketKeeper,
	bankKeeper types.BankKeeper,
	accountKeeper types.AccountKeeper,
	uregistryKeeper types.UregistryKeeper,
	utvKeeper types.UtvKeeper,
	uvalidatorKeeper types.UValidatorKeeper,
>>>>>>>> feat/universal-validator:x/ue/keeper/keeper.go
=======
>>>>>>> feat/universal-validator
) Keeper {
	logger = logger.With(log.ModuleKey, "x/"+types.ModuleName)

	sb := collections.NewSchemaBuilder(storeService)

	if authority == "" {
		authority = authtypes.NewModuleAddress(govtypes.ModuleName).String()
	}

	k := Keeper{
		cdc:    cdc,
		logger: logger,

		Params:       collections.NewItem(sb, types.ParamsKey, types.ParamsName, codec.CollValue[types.Params](cdc)),
<<<<<<< HEAD
<<<<<<<< HEAD:x/uregistry/keeper/keeper.go
=======
>>>>>>> feat/universal-validator
		ChainConfigs: collections.NewMap(sb, types.ChainConfigsKey, types.ChainConfigsName, collections.StringKey, codec.CollValue[types.ChainConfig](cdc)),
		TokenConfigs: collections.NewMap(sb, types.TokenConfigsKey, types.TokenConfigsName, collections.StringKey, codec.CollValue[types.TokenConfig](cdc)),

		authority: authority,
<<<<<<< HEAD
========

		authority:        authority,
		evmKeeper:        evmKeeper,
		feemarketKeeper:  feemarketKeeper,
		bankKeeper:       bankKeeper,
		accountKeeper:    accountKeeper,
		uregistryKeeper:  uregistryKeeper,
		utvKeeper:        utvKeeper,
		uvalidatorKeeper: uvalidatorKeeper,

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
>>>>>>>> feat/universal-validator:x/ue/keeper/keeper.go
=======
>>>>>>> feat/universal-validator
	}

	return k
}

func (k Keeper) Logger() log.Logger {
	return k.logger
}

// InitGenesis initializes the module's state from a genesis state.
func (k *Keeper) InitGenesis(ctx context.Context, data *types.GenesisState) error {

<<<<<<< HEAD
	if err := data.Params.Validate(); err != nil {
=======
	if err := data.Params.ValidateBasic(); err != nil {
>>>>>>> feat/universal-validator
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

<<<<<<< HEAD
func (k *Keeper) GetUeModuleAddress(ctx context.Context) (common.Address, string) {
	ueModuleAcc := k.accountKeeper.GetModuleAccount(ctx, types.ModuleName) // "ue"
	ueModuleAddr := ueModuleAcc.GetAddress()
	var ethSenderUEAddr common.Address
	copy(ethSenderUEAddr[:], ueModuleAddr.Bytes())

<<<<<<<< HEAD:x/uregistry/keeper/keeper.go
=======
func (k Keeper) GetChainConfig(ctx context.Context, chain string) (types.ChainConfig, error) {
	config, err := k.ChainConfigs.Get(ctx, chain)
	if err != nil {
		return types.ChainConfig{}, err
	}
	return config, nil
}

>>>>>>> feat/universal-validator
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
<<<<<<< HEAD
========
	return ethSenderUEAddr, ethSenderUEAddr.Hex()
>>>>>>>> feat/universal-validator:x/ue/keeper/keeper.go
=======
>>>>>>> feat/universal-validator
}
