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

	modulev1 "github.com/rollchains/pchain/api/ue/module/v1"
	"github.com/rollchains/pchain/x/ue/keeper"
	"github.com/rollchains/pchain/x/ue/types"
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

	StakingKeeper   stakingkeeper.Keeper
	SlashingKeeper  slashingkeeper.Keeper
	EVMKeeper       types.EVMKeeper
	FeeMarketKeeper types.FeeMarketKeeper
	BankKeeper      types.BankKeeper
	AccountKeeper   types.AccountKeeper
	UtvKeeper       types.UtvKeeper
}

type ModuleOutputs struct {
	depinject.Out

	Module appmodule.AppModule
	Keeper keeper.Keeper
}

func ProvideModule(in ModuleInputs) ModuleOutputs {
	govAddr := authtypes.NewModuleAddress(govtypes.ModuleName).String()

	k := keeper.NewKeeper(in.Cdc, in.StoreService, log.NewLogger(os.Stderr), govAddr, in.EVMKeeper, in.FeeMarketKeeper, in.BankKeeper, in.AccountKeeper, in.UtvKeeper)
	m := NewAppModule(in.Cdc, k, in.EVMKeeper, in.FeeMarketKeeper, in.BankKeeper, in.AccountKeeper, in.UtvKeeper)

	return ModuleOutputs{Module: m, Keeper: k, Out: depinject.Out{}}
}
