package testutils

import (
	"fmt"
	"testing"

	sdkmath "cosmossdk.io/math"

	log "cosmossdk.io/log"
	wasmkeeper "github.com/CosmWasm/wasmd/x/wasm/keeper"
	dbm "github.com/cosmos/cosmos-db"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
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

func SetAppWithMultipleValidators(t *testing.T, numVals int) (*app.ChainApp, sdk.Context, []sdk.AccountI, []stakingtypes.Validator) {
	app := SetupApp(t)

	ctx := app.BaseApp.NewContext(true)

	params, err := app.StakingKeeper.GetParams(ctx)
	require.NoError(t, err)
	params.BondDenom = "upc" // must match your token denom
	app.StakingKeeper.SetParams(ctx, params)
	// initialize distribution FeePool
	app.DistrKeeper.FeePool.Set(ctx, distrtypes.FeePool{})

	// configure EVM params for PUSH0 opcode
	configureEVMParams(app, ctx)

	appOptions := AppSetupOptions{
		TestConfig: GetDefaultTestConfig(),
		Addresses:  GetDefaultAddresses(),
	}
	accounts := SetupTestAccounts(t, app, ctx, appOptions)

	// Collect all accounts we’ll use as validators (for now re-use Default/Cosmos/Target,
	// but extend this with dynamically created accounts)
	baseAccounts := []sdk.AccountI{accounts.DefaultAccount, accounts.CosmosAccount, accounts.TargetAccount}

	// If numVals > 3, create extra accounts
	for i := len(baseAccounts); i < numVals; i++ {
		addr := sdk.AccAddress([]byte(fmt.Sprintf("val-extra-%d", i)))
		acc := createAndFundAccount(t, app, ctx, addr, appOptions.TestConfig.DefaultCoinAmt, appOptions.TestConfig.BaseCoinDenom)
		baseAccounts = append(baseAccounts, acc)
	}

	validators, pubkeys := SetupValidators(t, app, ctx, baseAccounts, numVals)

	for _, val := range validators {
		valOp, err := sdk.ValAddressFromBech32(val.GetOperator())
		require.NoError(t, err)
		err = app.DistrKeeper.SetValidatorOutstandingRewards(ctx, valOp, distrtypes.ValidatorOutstandingRewards{})
		require.NoError(t, err)
		app.DistrKeeper.SetValidatorAccumulatedCommission(ctx, valOp, distrtypes.ValidatorAccumulatedCommission{})
		hr := distrtypes.NewValidatorHistoricalRewards(sdk.DecCoins{}, 1)
		app.DistrKeeper.SetValidatorHistoricalRewards(ctx, valOp, 0, hr)
		cr := distrtypes.NewValidatorCurrentRewards(sdk.DecCoins{}, 1)
		app.DistrKeeper.SetValidatorCurrentRewards(ctx, valOp, cr)
	}

	// Set proposer as first validator's consensus address
	ctx = ctx.WithProposer(sdk.ConsAddress(pubkeys[0].Address()).Bytes())

	if err := setupUESystem(t, app, ctx, appOptions, accounts); err != nil {
		require.NoError(t, err)
	}

	return app, ctx, baseAccounts, validators
}

func configureEVMParams(app *app.ChainApp, ctx sdk.Context) {
	evmParams := app.EVMKeeper.GetParams(ctx)
	app.EVMKeeper.EnableEIPs(ctx, 3855)
	app.EVMKeeper.SetParams(ctx, evmParams)

	baseFee := sdkmath.NewInt(1000000000000000000)                  // Int
	app.FeeMarketKeeper.SetBaseFee(ctx, sdkmath.LegacyDec(baseFee)) // Dec
}
