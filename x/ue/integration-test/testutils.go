package integrationtest

import (
	"fmt"
	"testing"

	log "cosmossdk.io/log"
	wasmkeeper "github.com/CosmWasm/wasmd/x/wasm/keeper"
	dbm "github.com/cosmos/cosmos-db"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/rollchains/pchain/app"
	"github.com/stretchr/testify/require"

	"github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
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

	addr_cosmos := sdk.MustAccAddressFromBech32("cosmos18pjnzwr9xdnx2vnpv5mxywfnv56xxef5cludl5")

	acc_cosmos := app.AccountKeeper.NewAccountWithAddress(ctx, addr_cosmos)
	app.AccountKeeper.SetAccount(ctx, acc_cosmos)

	pk := ed25519.GenPrivKey().PubKey()

	valAddr := sdk.ValAddress(addr)

	validator, err := stakingtypes.NewValidator(valAddr.String(), pk, stakingtypes.Description{})
	require.NoError(t, err)
	app.StakingKeeper.SetValidator(ctx, validator)
	app.StakingKeeper.SetValidatorByConsAddr(ctx, validator)
	app.StakingKeeper.SetNewValidatorByPowerIndex(ctx, validator)
	ctx = ctx.WithProposer(sdk.ConsAddress(pk.Address()).Bytes())

	fmt.Println(app.StakingKeeper.GetValidatorByConsAddr(ctx, sdk.ConsAddress(pk.Address())))

	acc_all := app.AccountKeeper.GetAllAccounts(ctx)
	fmt.Println(acc_all)

	contractAddr := common.HexToAddress("0x00000000000000000000000000000000000000ea")
	evmAcc := app.EVMKeeper.GetAccountOrEmpty(ctx, contractAddr)
	code := app.EVMKeeper.GetCode(ctx, common.BytesToHash(evmAcc.CodeHash))

	fmt.Println("Hmmmmmmmmm : ", code)

	return app, ctx, acc
}
