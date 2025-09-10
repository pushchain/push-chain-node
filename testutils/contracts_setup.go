package testutils

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/pushchain/push-chain-node/app"
	uetypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
	"github.com/stretchr/testify/require"
)

func setupUESystem(
	t *testing.T,
	app *app.ChainApp,
	ctx sdk.Context,
	opts AppSetupOptions,
	accounts TestAccounts,
) error {
	// Initialize UE genesis
	app.UexecutorKeeper.InitGenesis(ctx, &uetypes.GenesisState{})

	// Parse factory ABI
	factoryABI, err := uetypes.ParseFactoryABI()
	require.NoError(t, err)

	// Setup factory contract
	err = setupFactoryContract(t, app, ctx, factoryABI, opts, accounts)
	require.NoError(t, err)

	// Register chain and UEA
	err = registerEVMChainAndUEA(t, app, ctx, factoryABI, opts, accounts)
	require.NoError(t, err)

	// Parse Handler ABI
	handlerABI, err := uetypes.ParseHandlerABI()
	require.NoError(t, err)

	// Setup Handler contract
	err = setupHandlerContract(t, app, ctx, handlerABI, opts, accounts)
	require.NoError(t, err)

	// Parse PRC20 ABI
	prc20ABI, err := uetypes.ParsePRC20ABI()
	require.NoError(t, err)

	// Setup Handler contract
	err = setupPrc20Contract(t, app, ctx, prc20ABI, opts, accounts)
	require.NoError(t, err)

	return nil
}

func setupHandlerContract(
	t *testing.T,
	app *app.ChainApp,
	ctx sdk.Context,
	handlerABI abi.ABI,
	opts AppSetupOptions,
	accounts TestAccounts,
) error {
	handlerAddr := opts.Addresses.HandlerAddr
	owner := common.BytesToAddress(accounts.DefaultAccount.GetAddress().Bytes())

	// Deploy Handler contract
	_ = DeployContract(
		t,
		app,
		ctx,
		handlerAddr,
		HANDLER_CONTRACT_BYTECODE,
	)

	const (
		WPCAddress              = "0x1111111111111111111111111111111111111111"
		UniswapV3FactoryAddress = "0x2222222222222222222222222222222222222222"
		UniswapV3RouterAddress  = "0x3333333333333333333333333333333333333333"
		UniswapV3QuoterAddress  = "0x4444444444444444444444444444444444444444"
	)

	// Set UEA proxy implementation
	_, err := app.EVMKeeper.CallEVM(
		ctx,
		handlerABI,
		owner,
		handlerAddr,
		true,
		"initialize",
		common.HexToAddress(WPCAddress),
		common.HexToAddress(UniswapV3FactoryAddress),
		common.HexToAddress(UniswapV3RouterAddress),
		common.HexToAddress(UniswapV3QuoterAddress),
	)
	require.NoError(t, err)
	return nil
}

func setupFactoryContract(
	t *testing.T,
	app *app.ChainApp,
	ctx sdk.Context,
	factoryABI abi.ABI,
	opts AppSetupOptions,
	accounts TestAccounts,
) error {
	factoryAddr := opts.Addresses.FactoryAddr
	owner := common.BytesToAddress(accounts.DefaultAccount.GetAddress().Bytes())

	// Check initial factory owner
	ownerResult, err := app.EVMKeeper.CallEVM(ctx, factoryABI, owner, factoryAddr, true, "owner")
	require.NoError(t, err)
	t.Logf("Factory owner after genesis: %s", common.BytesToAddress(ownerResult.Ret).Hex())

	// Initialize factory with owner
	_, err = app.EVMKeeper.CallEVM(ctx, factoryABI, owner, factoryAddr, true, "initialize", owner)
	require.NoError(t, err)

	// Verify owner is set
	ownerResult, err = app.EVMKeeper.CallEVM(ctx, factoryABI, owner, factoryAddr, true, "owner")
	require.NoError(t, err)
	t.Logf("Factory owner after initialization: %s", common.BytesToAddress(ownerResult.Ret).Hex())

	// Deploy UE Proxy
	ProxyAddress := DeployContract(
		t,
		app,
		ctx,
		opts.Addresses.UEProxyAddr,
		UEA_PROXY_BYTECODE,
	)

	// Set UEA proxy implementation
	receipt, err := app.EVMKeeper.CallEVM(
		ctx,
		factoryABI,
		owner,
		factoryAddr,
		true,
		"setUEAProxyImplementation",
		ProxyAddress,
	)
	require.NoError(t, err)
	t.Logf("UEA Proxy implementation set. Receipt: %v", receipt)

	return nil
}

func setupPrc20Contract(
	t *testing.T,
	app *app.ChainApp,
	ctx sdk.Context,
	prc20ABI abi.ABI,
	opts AppSetupOptions,
	accounts TestAccounts,
) error {
	prc20Addr := opts.Addresses.PRC20USDCAddr
	ueModuleAccAddress, _ := app.UexecutorKeeper.GetUeModuleAddress(ctx)

	// Deploy Handler contract
	_ = DeployContract(
		t,
		app,
		ctx,
		prc20Addr,
		PRC20_CREATION_BYTECODE,
	)

	// Set UEA proxy implementation
	_, err := app.EVMKeeper.CallEVM(
		ctx,
		prc20ABI,
		ueModuleAccAddress,
		prc20Addr,
		true,
		"updateHandlerContract",
		opts.Addresses.HandlerAddr,
	)
	require.NoError(t, err)
	return nil
}

func registerEVMChainAndUEA(
	t *testing.T,
	chainApp *app.ChainApp,
	ctx sdk.Context,
	factoryABI abi.ABI,
	opts AppSetupOptions,
	accounts TestAccounts,
) error {
	factoryAddr := opts.Addresses.FactoryAddr
	owner := common.BytesToAddress(accounts.DefaultAccount.GetAddress().Bytes())

	// Compute chain hashes
	EVMHash := crypto.Keccak256Hash([]byte("EVM"))

	chainArgs := abi.Arguments{
		{Type: abi.Type{T: abi.StringTy}},
		{Type: abi.Type{T: abi.StringTy}},
	}
	packed, err := chainArgs.Pack("eip155", "11155111")
	require.NoError(t, err)

	ChainHash := crypto.Keccak256Hash(packed)

	t.Logf("Computed chainHash: %s", ChainHash.Hex())

	// Register new chain
	_, err = chainApp.EVMKeeper.CallEVM(
		ctx,
		factoryABI,
		owner,
		factoryAddr,
		true,
		"registerNewChain",
		ChainHash,
		EVMHash,
	)
	require.NoError(t, err)

	// Deploy EVM implementation
	EVMImplAddress := DeployContract(
		t,
		chainApp,
		ctx,
		opts.Addresses.EVMImplAddr,
		UEA_EVM_BYTECODE,
	)

	// Register UEA
	_, err = chainApp.EVMKeeper.CallEVM(
		ctx,
		factoryABI,
		owner,
		factoryAddr,
		true,
		"registerUEA",
		ChainHash,
		EVMHash,
		EVMImplAddress,
	)
	require.NoError(t, err)

	// Get UEA address
	ueaAddrResult, err := chainApp.EVMKeeper.CallEVM(
		ctx,
		factoryABI,
		owner,
		factoryAddr,
		true,
		"getUEA",
		ChainHash,
	)
	require.NoError(t, err)

	UEAAddress := common.BytesToAddress(ueaAddrResult.Ret)
	t.Logf("UEA registered at: %s", UEAAddress.Hex())

	return nil
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
