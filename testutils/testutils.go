package testutils

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

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"

	uetypes "github.com/rollchains/pchain/x/ue/types"
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

	evmParams := app.EVMKeeper.GetParams(ctx)
	evmParams.ExtraEIPs = []string{"ethereum_3855"} // Enable PUSH0 opcode
	app.EVMKeeper.SetParams(ctx, evmParams)

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
	factoryAddr := common.HexToAddress("0x00000000000000000000000000000000000000ea")
	factoryABI, err := uetypes.ParseFactoryABI()

	// the account you want to fund
	targetAddr := sdk.AccAddress([]byte("\x86i\xbe\xd1!\xfe\xfa=\x9c\xf2\x82\x12s\xf4\x89\xe7\x17̩]"))

	// ------------------------------------------for execute payload--------------------------------------------------------
	coins = sdk.NewCoins(sdk.NewInt64Coin("upc", 23748000000000)) // or more
	acc = app.AccountKeeper.NewAccountWithAddress(ctx, targetAddr)
	app.AccountKeeper.SetAccount(ctx, acc)

	err = app.BankKeeper.MintCoins(ctx, "mint", coins)
	require.NoError(t, err)

	err = app.BankKeeper.SendCoinsFromModuleToAccount(ctx, "mint", targetAddr, coins)
	require.NoError(t, err)

	// ------------------------------------------for execute payload--------------------------------------------------------
	app.UeKeeper.InitGenesis(ctx, &uetypes.GenesisState{})
	ownerAddr, err := app.EVMKeeper.CallEVM(ctx, factoryABI, common.BytesToAddress(addr.Bytes()), factoryAddr, true, "owner")
	fmt.Println("Factory owner after genesis:", common.BytesToAddress(ownerAddr.Ret))

	owner := common.BytesToAddress(addr.Bytes())
	app.EVMKeeper.CallEVM(ctx, factoryABI, common.BytesToAddress(addr.Bytes()), factoryAddr, true, "initialize", owner)

	require.NoError(t, err)
	ownerAd, err := app.EVMKeeper.CallEVM(ctx, factoryABI, common.BytesToAddress(addr.Bytes()), factoryAddr, true, "owner")
	fmt.Println("Owner now : ", ownerAd)

	ueProxyAddress := DeployContract(t, app, ctx, common.HexToAddress("0x0000000000000000000000000000000000000e09"), UEA_PROXY_BYTECODE)
	receipt, err := app.EVMKeeper.CallEVM(ctx, factoryABI, owner, factoryAddr, true, "setUEAProxyImplementation", ueProxyAddress)
	require.NoError(t, err)
	fmt.Println("Proxy receipt : ", receipt)

	evmHash := crypto.Keccak256Hash([]byte("EVM"))

	chainArgs := abi.Arguments{
		{Type: abi.Type{T: abi.StringTy}},
		{Type: abi.Type{T: abi.StringTy}},
	}
	packed, err := chainArgs.Pack("eip155", "11155111")
	require.NoError(t, err)

	evmSepoliaHash := crypto.Keccak256Hash(packed)
	fmt.Println("Computed chainHash:", evmSepoliaHash.Hex())

	receipt, err = app.EVMKeeper.CallEVM(
		ctx,
		factoryABI,
		common.BytesToAddress(addr.Bytes()), // from
		factoryAddr,                         // contract
		true,                                // commit (stateful tx)
		"registerNewChain",
		evmSepoliaHash, // arg1: bytes32
		evmHash,        // arg2: bytes32
	)
	require.NoError(t, err)

	evmImplAddr := DeployContract(t, app, ctx, common.HexToAddress("0x0000000000000000000000000000000000000e01"), UEA_EVM_BYTECODE)

	receipt, err = app.EVMKeeper.CallEVM(
		ctx,
		factoryABI,
		owner,       // must equal the factory owner
		factoryAddr, // 0x...ea
		true,
		"registerUEA",
		evmSepoliaHash,
		evmHash,
		evmImplAddr,
	)
	require.NoError(t, err)

	ueaAddrBytes, err := app.EVMKeeper.CallEVM(ctx, factoryABI, owner, factoryAddr, true, "getUEA", evmSepoliaHash)
	require.NoError(t, err)

	ueaAddr := common.BytesToAddress(ueaAddrBytes.Ret)
	fmt.Println("UEA registered at:", ueaAddr.Hex())

	return app, ctx, acc
}

func DeployContract(
	t *testing.T,
	app *app.ChainApp,
	ctx sdk.Context,
	contractAddr common.Address,
	bytecodeHex string,
) common.Address {
	bytecode, err := hexutil.Decode("0x" + bytecodeHex)
	require.NoError(t, err)

	codeHash := crypto.Keccak256Hash(bytecode)

	evmAcc := app.EVMKeeper.GetAccountOrEmpty(ctx, contractAddr)
	evmAcc.CodeHash = codeHash.Bytes()
	app.EVMKeeper.SetAccount(ctx, contractAddr, evmAcc)
	app.EVMKeeper.SetCode(ctx, codeHash.Bytes(), bytecode)

	return contractAddr
}
