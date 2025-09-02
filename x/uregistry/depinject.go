package module

import (
	"os"

	"github.com/cosmos/cosmos-sdk/codec"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	slashingkeeper "github.com/cosmos/cosmos-sdk/x/slashing/keeper"

	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"

	"cosmossdk.io/core/address"
	"cosmossdk.io/core/appmodule"
	"cosmossdk.io/core/store"
	"cosmossdk.io/depinject"
	"cosmossdk.io/log"

	modulev1 "github.com/pushchain/push-chain-node/api/uregistry/module/v1"
	"github.com/pushchain/push-chain-node/x/uregistry/keeper"
)

var _ appmodule.AppModule = AppModule{}

// IsOnePerModuleType implements the depinject.OnePerModuleType interface.
func (am AppModule) IsOnePerModuleType() {}

// IsAppModule implements the appmodule.AppModule interface.
func (am AppModule) IsAppModule() {}

func init() {
	appmodule.Register(
		&modulev1.Module{},
		appmodule.Provide(ProvideModule),
	)
}

type ModuleInputs struct {
	depinject.In

	Cdc          codec.Codec
	StoreService store.KVStoreService
	AddressCodec address.Codec

<<<<<<< HEAD
<<<<<<<< HEAD:x/uregistry/depinject.go
	StakingKeeper  stakingkeeper.Keeper
	SlashingKeeper slashingkeeper.Keeper
========
	StakingKeeper   stakingkeeper.Keeper
	SlashingKeeper  slashingkeeper.Keeper
	UregistryKeeper types.UregistryKeeper
>>>>>>>> feat/universal-validator:x/utv/depinject.go
=======
	StakingKeeper  stakingkeeper.Keeper
	SlashingKeeper slashingkeeper.Keeper
>>>>>>> feat/universal-validator
}

type ModuleOutputs struct {
	depinject.Out

	Module appmodule.AppModule
	Keeper keeper.Keeper
}

func ProvideModule(in ModuleInputs) ModuleOutputs {
	govAddr := authtypes.NewModuleAddress(govtypes.ModuleName).String()

<<<<<<< HEAD
<<<<<<<< HEAD:x/uregistry/depinject.go
	k := keeper.NewKeeper(in.Cdc, in.StoreService, log.NewLogger(os.Stderr), govAddr)
	m := NewAppModule(in.Cdc, k)
========
	k := keeper.NewKeeper(in.Cdc, in.StoreService, log.NewLogger(os.Stderr), govAddr, in.UregistryKeeper)
	m := NewAppModule(in.Cdc, k, in.UregistryKeeper)
>>>>>>>> feat/universal-validator:x/utv/depinject.go
=======
	k := keeper.NewKeeper(in.Cdc, in.StoreService, log.NewLogger(os.Stderr), govAddr)
	m := NewAppModule(in.Cdc, k)
>>>>>>> feat/universal-validator

	return ModuleOutputs{Module: m, Keeper: k, Out: depinject.Out{}}
}
