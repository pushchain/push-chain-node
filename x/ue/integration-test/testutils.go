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

func SetAppWithValidators(t *testing.T) (*app.ChainApp, sdk.Context) {
	app := SetupApp(t)

	ctx := app.BaseApp.NewContext(true)

	addr := sdk.AccAddress([]byte("testaddr1"))
	coins := sdk.NewCoins(sdk.NewInt64Coin("upc", 100000000)) // creates upc coins
	acc := app.AccountKeeper.NewAccountWithAddress(ctx, addr) // creates account on our app
	app.AccountKeeper.SetAccount(ctx, acc)

	err := app.BankKeeper.MintCoins(ctx, "mint", coins)
	require.NoError(t, err)

	err = app.BankKeeper.SendCoinsFromModuleToAccount(ctx, "mint", addr, coins)
	require.NoError(t, err)

	return app, ctx
}

// func TestMintPC(t *testing.T) {
// 	app, ctx := SetAppWithValidators(t)

// 	// create addr
// 	acc := simtestutil.CreateIncrementalAccounts(3)
// 	validSigner := acc[0]

// 	validUA := &uetypes.UniversalAccountId{
// 		ChainNamespace: "eip155",
// 		ChainId:        "11155111",
// 		Owner:          "0x000000000000000000000000000000000000dead",
// 	}

// 	validTxHash := "0xabc123"

// 	msg := &uetypes.MsgMintPC{
// 		Signer:             validSigner.String(),
// 		UniversalAccountId: validUA,
// 		TxHash:             validTxHash,
// 	}

// 	//addr := common.HexToAddress("0x1234567890abcdef1234567890abcdef12345678")

// 	// padded := common.LeftPadBytes(addr.Bytes(), 32)
// 	// receipt := &evmtypes.MsgEthereumTxResponse{
// 	// 	Ret: padded,
// 	// }

// 	// usdAmount := new(big.Int)
// 	// usdAmount.SetString("1000000000000000000", 10) // 10 USD, 18 decimals
// 	// decimals := uint32(18)
// 	// amountToMint := uekeeper.ConvertUsdToPCTokens(usdAmount, decimals)
// 	// expectedCoins := sdk.NewCoins(sdk.NewCoin(pchaintypes.BaseDenom, amountToMint))

// 	_, evmFromAddress, err := utils.GetAddressPair(msg.Signer)
// 	require.NoError(t, err)

// 	fmt.Println("hello world ")

// 	app.UeKeeper.MintPC(ctx, evmFromAddress, msg.UniversalAccountId, validTxHash)

// }
