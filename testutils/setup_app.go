package testutils

import (
	"testing"

	log "cosmossdk.io/log"
	wasmkeeper "github.com/CosmWasm/wasmd/x/wasm/keeper"
	dbm "github.com/cosmos/cosmos-db"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/pushchain/push-chain-node/app"
	"github.com/stretchr/testify/require"
)

type AppSetupOptions struct {
	TestConfig TestConfig
	Addresses  Addresses
}

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
	//configure EVM params for PUSH0 opcode
	configureEVMParams(app, ctx)

	appOptions := AppSetupOptions{
		TestConfig: GetDefaultTestConfig(),
		Addresses:  GetDefaultAddresses(),
	}
	accounts := SetupTestAccounts(t, app, ctx, appOptions)

	_, pk := setupValidator(t, app, ctx, accounts.DefaultAccount)
	ctx = ctx.WithProposer(sdk.ConsAddress(pk.Address()).Bytes())

	if err := setupUESystem(t, app, ctx, appOptions, accounts); err != nil {
		require.NoError(t, err)
	}

	return app, ctx, accounts.DefaultAccount
}

func configureEVMParams(app *app.ChainApp, ctx sdk.Context) {
	evmParams := app.EVMKeeper.GetParams(ctx)
	evmParams.ExtraEIPs = []string{"ethereum_3855"}
	app.EVMKeeper.SetParams(ctx, evmParams)
}
