package integrationtest

import (
	"fmt"
	"math/big"
	"strings"
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
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"

	uetypes "github.com/rollchains/pchain/x/ue/types"
	// "github.com/rollchains/pchain/utils"
	// uetypes "github.com/rollchains/pchain/x/ue/types"
)

const FactoryABI = `[
  {
    "type": "function",
    "name": "initialize",
    "inputs": [
      { "name": "initialOwner", "type": "address", "internalType": "address" }
    ],
    "outputs": [],
    "stateMutability": "nonpayable"
  },
  {
    "type": "function",
    "name": "registerNewChain",
    "inputs": [
      { "name": "_chainHash", "type": "bytes32", "internalType": "bytes32" },
      { "name": "_vmHash", "type": "bytes32", "internalType": "bytes32" }
    ],
    "outputs": [],
    "stateMutability": "nonpayable"
  },
  {
    "type": "function",
    "name": "deployUEA",
    "inputs": [
      {
        "name": "_id",
        "type": "tuple",
        "internalType": "struct UniversalAccountId",
        "components": [
          { "name": "chainNamespace", "type": "string", "internalType": "string" },
          { "name": "chainId", "type": "string", "internalType": "string" },
          { "name": "owner", "type": "bytes", "internalType": "bytes" }
        ]
      }
    ],
    "outputs": [
      { "name": "", "type": "address", "internalType": "address" }
    ],
    "stateMutability": "nonpayable"
  },
  {
    "type": "function",
    "name": "computeUEA",
    "inputs": [
      {
        "name": "_id",
        "type": "tuple",
        "internalType": "struct UniversalAccountId",
        "components": [
          { "name": "chainNamespace", "type": "string", "internalType": "string" },
          { "name": "chainId", "type": "string", "internalType": "string" },
          { "name": "owner", "type": "bytes", "internalType": "bytes" }
        ]
      }
    ],
    "outputs": [
      { "name": "", "type": "address", "internalType": "address" }
    ],
    "stateMutability": "view"
  }
]`

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

	acc_all := app.AccountKeeper.GetAllAccounts(ctx)
	fmt.Println(acc_all)

	app.UeKeeper.InitGenesis(ctx, &uetypes.GenesisState{})

	factoryAddr := common.HexToAddress("0x00000000000000000000000000000000000000ea")
	evmAcc := app.EVMKeeper.GetAccountOrEmpty(ctx, factoryAddr)
	code := app.EVMKeeper.GetCode(ctx, common.BytesToHash(evmAcc.CodeHash))

	factoryABI, err := abi.JSON(strings.NewReader(FactoryABI))

	// 4. Call factory.initialize(owner)
	owner := common.BytesToAddress(addr.Bytes())
	app.EVMKeeper.CallEVM(ctx, factoryABI, common.BytesToAddress(addr.Bytes()), factoryAddr, false, "initialize", owner)

	require.NoError(t, err)

	ownerAddr, _ := CallContractMethod(t, app, ctx, addr, factoryAddr, factoryABI, "getOwner")
	fmt.Println("Stored owner:", common.BytesToAddress(ownerAddr))

	evmHash := crypto.Keccak256Hash([]byte("EVM"))
	evmSepoliaHash := crypto.Keccak256Hash([]byte("eip15511155111"))

	app.EVMKeeper.CallEVM(
		ctx,
		factoryABI,
		common.BytesToAddress(addr.Bytes()), // from
		factoryAddr,                         // contract
		false,                               // commit (stateful tx)
		"registerNewChain",
		[32]byte(evmSepoliaHash), // arg1: bytes32
		[32]byte(evmHash),        // arg2: bytes32
	)
	require.NoError(t, err)

	evmImplAddr := DeployContract(t, app, ctx, common.HexToAddress("0x0000000000000000000000000000000000000e01"), UEA_EVM_BYTECODE)

	_, err = CallContractMethod(t, app, ctx, addr, factoryAddr, factoryABI, "registerUEA", evmSepoliaHash, evmHash, evmImplAddr)
	require.NoError(t, err)
	fmt.Println("Hmmmmmmmmm : ", code)

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

func CallContractMethod(t *testing.T, app *app.ChainApp, ctx sdk.Context, from sdk.AccAddress, contract common.Address, contractABI abi.ABI, method string, args ...interface{}) ([]byte, error) {
	input, err := contractABI.Pack(method, args...)
	nonce := app.EVMKeeper.GetNonce(ctx, common.BytesToAddress(from.Bytes()))

	msg := ethtypes.NewMessage(
		common.BytesToAddress(from.Bytes()), // from
		&contract,                           // to
		nonce,                               // nonce
		big.NewInt(0),                       // no value sent
		5_000_000,                           // gas limit
		big.NewInt(1_000_000_000),           // gas price
		nil, nil,                            // no fee caps
		input, // data (encoded method)
		nil,   // access list
		true,  // check nonce
	)

	res, err := app.EVMKeeper.ApplyMessage(ctx, msg, nil, false)
	if err != nil {
		return nil, err
	}

	return res.Ret, nil
}
