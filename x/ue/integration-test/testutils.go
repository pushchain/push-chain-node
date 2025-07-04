package integrationtest

import (
	"testing"

	log "cosmossdk.io/log"
	wasmkeeper "github.com/CosmWasm/wasmd/x/wasm/keeper"
	dbm "github.com/cosmos/cosmos-db"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/rollchains/pchain/app"
	"github.com/stretchr/testify/require"
	// "github.com/rollchains/pchain/utils"
	// uetypes "github.com/rollchains/pchain/x/ue/types"
)

func SetupApp(t *testing.T) *app.ChainApp {
	db := dbm.NewMemDB()
	logger := log.NewTestLogger(t)
	var wasmOpts []wasmkeeper.Option = nil

	pcApp := app.NewChainApp(logger, db, nil, true, simtestutil.NewAppOptionsWithFlagHome(t.TempDir()), wasmOpts, app.EVMAppOptions)

	return pcApp
}

func SetAppWithValidators(t *testing.T) (*app.ChainApp, sdk.Context, sdk.AccountI) {
	app := SetupApp(t)

	ctx := app.BaseApp.NewContext(true)

	addr := sdk.AccAddress([]byte("0x778d3206374f8ac265728e18e3fe2ae6b93e4ce4"))
	coins := sdk.NewCoins(sdk.NewInt64Coin("upc", 100000000)) // creates upc coins
	acc := app.AccountKeeper.NewAccountWithAddress(ctx, addr) // creates account on our app
	app.AccountKeeper.SetAccount(ctx, acc)

	err := app.BankKeeper.MintCoins(ctx, "mint", coins)
	require.NoError(t, err)

	err = app.BankKeeper.SendCoinsFromModuleToAccount(ctx, "mint", addr, coins)
	require.NoError(t, err)

	return app, ctx, acc
}
