package keeper_test

import (
	"os"
	"testing"

	"github.com/stretchr/testify/suite"

	"cosmossdk.io/core/address"
	"cosmossdk.io/log"
	storetypes "cosmossdk.io/store/types"

	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	sdkaddress "github.com/cosmos/cosmos-sdk/codec/address"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/testutil/integration"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	authkeeper "github.com/cosmos/cosmos-sdk/x/auth/keeper"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	mintkeeper "github.com/cosmos/cosmos-sdk/x/mint/keeper"
	minttypes "github.com/cosmos/cosmos-sdk/x/mint/types"
	stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	"github.com/push-protocol/push-chain/app"
	"github.com/push-protocol/push-chain/utils/env"
	module "github.com/push-protocol/push-chain/x/utv"
	"github.com/push-protocol/push-chain/x/utv/keeper"
	"github.com/push-protocol/push-chain/x/utv/types"
)

var maccPerms = map[string][]string{
	authtypes.FeeCollectorName:     nil,
	stakingtypes.BondedPoolName:    {authtypes.Burner, authtypes.Staking},
	stakingtypes.NotBondedPoolName: {authtypes.Burner, authtypes.Staking},
	minttypes.ModuleName:           {authtypes.Minter},
	govtypes.ModuleName:            {authtypes.Burner},
}

type testFixture struct {
	suite.Suite

	ctx         sdk.Context
	k           keeper.Keeper
	msgServer   types.MsgServer
	queryServer types.QueryServer
	appModule   *module.AppModule

	accountkeeper authkeeper.AccountKeeper
	bankkeeper    bankkeeper.BaseKeeper
	stakingKeeper *stakingkeeper.Keeper
	mintkeeper    mintkeeper.Keeper

	addrs      []sdk.AccAddress
	govModAddr string
}

func SetupTest(t *testing.T) *testFixture {
	t.Helper()
	f := new(testFixture)

	cfg := sdk.GetConfig() // do not seal, more set later
	cfg.SetBech32PrefixForAccount(app.Bech32PrefixAccAddr, app.Bech32PrefixAccPub)
	cfg.SetBech32PrefixForValidator(app.Bech32PrefixValAddr, app.Bech32PrefixValPub)
	cfg.SetBech32PrefixForConsensusNode(app.Bech32PrefixConsAddr, app.Bech32PrefixConsPub)
	cfg.SetCoinType(app.CoinType)

	validatorAddressCodec := sdkaddress.NewBech32Codec(app.Bech32PrefixValAddr)
	accountAddressCodec := sdkaddress.NewBech32Codec(app.Bech32PrefixAccAddr)
	consensusAddressCodec := sdkaddress.NewBech32Codec(app.Bech32PrefixConsAddr)

	// Base setup
	logger := log.NewTestLogger(t)
	encCfg := moduletestutil.MakeTestEncodingConfig()

	f.govModAddr = authtypes.NewModuleAddress(govtypes.ModuleName).String()
	f.addrs = simtestutil.CreateIncrementalAccounts(3)

	keys := storetypes.NewKVStoreKeys(authtypes.ModuleName, banktypes.ModuleName, stakingtypes.ModuleName, minttypes.ModuleName, types.ModuleName)
	f.ctx = sdk.NewContext(integration.CreateMultiStore(keys, logger), cmtproto.Header{}, false, logger)

	// Register SDK modules.
	registerBaseSDKModules(logger, f, encCfg, keys, accountAddressCodec, validatorAddressCodec, consensusAddressCodec)

	// Setup Keeper.
	f.k = keeper.NewKeeper(encCfg.Codec, runtime.NewKVStoreService(keys[types.ModuleName]), logger, f.govModAddr)
	f.msgServer = keeper.NewMsgServerImpl(f.k)
	f.queryServer = keeper.NewQuerier(f.k)
	f.appModule = module.NewAppModule(encCfg.Codec, f.k)

	return f
}

func registerModuleInterfaces(encCfg moduletestutil.TestEncodingConfig) {
	authtypes.RegisterInterfaces(encCfg.InterfaceRegistry)
	stakingtypes.RegisterInterfaces(encCfg.InterfaceRegistry)
	banktypes.RegisterInterfaces(encCfg.InterfaceRegistry)
	minttypes.RegisterInterfaces(encCfg.InterfaceRegistry)

	types.RegisterInterfaces(encCfg.InterfaceRegistry)
}

func registerBaseSDKModules(
	logger log.Logger,
	f *testFixture,
	encCfg moduletestutil.TestEncodingConfig,
	keys map[string]*storetypes.KVStoreKey,
	ac address.Codec,
	validator address.Codec,
	consensus address.Codec,
) {
	registerModuleInterfaces(encCfg)

	// Auth Keeper.
	f.accountkeeper = authkeeper.NewAccountKeeper(
		encCfg.Codec, runtime.NewKVStoreService(keys[authtypes.StoreKey]),
		authtypes.ProtoBaseAccount,
		maccPerms,
		ac, app.Bech32PrefixAccAddr,
		f.govModAddr,
	)

	// Bank Keeper.
	f.bankkeeper = bankkeeper.NewBaseKeeper(
		encCfg.Codec, runtime.NewKVStoreService(keys[banktypes.StoreKey]),
		f.accountkeeper,
		nil,
		f.govModAddr, logger,
	)

	// Staking Keeper.
	f.stakingKeeper = stakingkeeper.NewKeeper(
		encCfg.Codec, runtime.NewKVStoreService(keys[stakingtypes.StoreKey]),
		f.accountkeeper, f.bankkeeper, f.govModAddr,
		validator,
		consensus,
	)

	// Mint Keeper.
	f.mintkeeper = mintkeeper.NewKeeper(
		encCfg.Codec, runtime.NewKVStoreService(keys[minttypes.StoreKey]),
		f.stakingKeeper, f.accountkeeper, f.bankkeeper,
		authtypes.FeeCollectorName, f.govModAddr,
	)
}

func TestKeeperTestSuite(t *testing.T) {
	// Load .env file before running tests
	env.LoadEnv() // This will only load once due to the IsLoaded check in the utility
	suite.Run(t, new(KeeperTestSuite))
}

type KeeperTestSuite struct {
	suite.Suite
	fixture *testFixture
}

func (suite *KeeperTestSuite) SetupTest() {
	suite.fixture = SetupTest(suite.T())
}

func (suite *KeeperTestSuite) TestAddChainConfig() {
	k := suite.fixture.k
	ctx := suite.fixture.ctx

	// Create a test chain config
	config := types.ChainConfigData{
		ChainId:               "1",
		ChainName:             "Ethereum Mainnet",
		CaipPrefix:            "eip155:1",
		LockerContractAddress: "0x1234567890AbCdEf1234567890AbCdEf12345678",
		UsdcAddress:           "0xabcdef1234567890AbCdEf1234567890AbCdEf12",
		PublicRpcUrl:          "https://ethereum-rpc.example.com",
	}

	// Test adding a chain config
	err := k.AddChainConfig(ctx, config)
	suite.Require().NoError(err)

	// Check if the chain config was added correctly
	retrievedConfig, err := k.GetChainConfig(ctx, config.ChainId)
	suite.Require().NoError(err)
	suite.Require().Equal(config.ChainId, retrievedConfig.ChainId)
	suite.Require().Equal(config.ChainName, retrievedConfig.ChainName)
	suite.Require().Equal(config.CaipPrefix, retrievedConfig.CaipPrefix)
	suite.Require().Equal(config.LockerContractAddress, retrievedConfig.LockerContractAddress)
	suite.Require().Equal(config.UsdcAddress, retrievedConfig.UsdcAddress)
	suite.Require().Equal(config.PublicRpcUrl, retrievedConfig.PublicRpcUrl)

	// Test adding a chain config with the same ID (should fail)
	err = k.AddChainConfig(ctx, config)
	suite.Require().NoError(err, "Adding the same config should update rather than error")

	// Test adding an invalid chain config
	invalidConfig := types.ChainConfigData{
		ChainId:               "", // Empty chain ID
		ChainName:             "Invalid Chain",
		CaipPrefix:            "invalid", // Invalid CAIP prefix
		LockerContractAddress: "0x123",
		UsdcAddress:           "0x456",
		PublicRpcUrl:          "https://invalid-chain.example.com",
	}

	err = k.AddChainConfig(ctx, invalidConfig)
	suite.Require().Error(err, "Adding an invalid chain config should fail")
}

func (suite *KeeperTestSuite) TestUpdateChainConfig() {
	k := suite.fixture.k
	ctx := suite.fixture.ctx

	// Create and add a test chain config
	config := types.ChainConfigData{
		ChainId:               "1",
		ChainName:             "Ethereum Mainnet",
		CaipPrefix:            "eip155:1",
		LockerContractAddress: "0x1234567890AbCdEf1234567890AbCdEf12345678",
		UsdcAddress:           "0xabcdef1234567890AbCdEf1234567890AbCdEf12",
		PublicRpcUrl:          "https://ethereum-rpc.example.com",
	}

	err := k.AddChainConfig(ctx, config)
	suite.Require().NoError(err)

	// Update the chain config
	updatedConfig := types.ChainConfigData{
		ChainId:               "1",
		ChainName:             "Ethereum Mainnet Updated",
		CaipPrefix:            "eip155:1",
		LockerContractAddress: "0x9876543210AbCdEf9876543210AbCdEf98765432",
		UsdcAddress:           "0xfedcba9876543210FeDcBa9876543210FeDcBa98",
		PublicRpcUrl:          "https://updated-ethereum-rpc.example.com",
	}

	err = k.AddChainConfig(ctx, updatedConfig)
	suite.Require().NoError(err)

	// Check if the chain config was updated correctly
	retrievedConfig, err := k.GetChainConfig(ctx, config.ChainId)
	suite.Require().NoError(err)
	suite.Require().Equal(updatedConfig.ChainId, retrievedConfig.ChainId)
	suite.Require().Equal(updatedConfig.ChainName, retrievedConfig.ChainName)
	suite.Require().Equal(updatedConfig.CaipPrefix, retrievedConfig.CaipPrefix)
	suite.Require().Equal(updatedConfig.LockerContractAddress, retrievedConfig.LockerContractAddress)
	suite.Require().Equal(updatedConfig.UsdcAddress, retrievedConfig.UsdcAddress)
	suite.Require().Equal(updatedConfig.PublicRpcUrl, retrievedConfig.PublicRpcUrl)

	// Test updating a non-existent chain config (should add it)
	newConfig := types.ChainConfigData{
		ChainId:               "137",
		ChainName:             "Polygon Mainnet",
		CaipPrefix:            "eip155:137",
		LockerContractAddress: "0x1234567890AbCdEf1234567890AbCdEf12345678",
		UsdcAddress:           "0xabcdef1234567890AbCdEf1234567890AbCdEf12",
		PublicRpcUrl:          "https://polygon-rpc.example.com",
	}

	err = k.AddChainConfig(ctx, newConfig)
	suite.Require().NoError(err)

	// Check if the new chain config was added correctly
	retrievedConfig, err = k.GetChainConfig(ctx, newConfig.ChainId)
	suite.Require().NoError(err)
	suite.Require().Equal(newConfig.ChainId, retrievedConfig.ChainId)
}

func (suite *KeeperTestSuite) TestGetChainConfigWithRPCOverride() {
	k := suite.fixture.k
	ctx := suite.fixture.ctx

	// Create and add a test chain config
	config := types.ChainConfigData{
		ChainId:               "1",
		ChainName:             "Ethereum Mainnet",
		CaipPrefix:            "eip155:1",
		LockerContractAddress: "0x1234567890AbCdEf1234567890AbCdEf12345678",
		UsdcAddress:           "0xabcdef1234567890AbCdEf1234567890AbCdEf12",
		PublicRpcUrl:          "https://ethereum-rpc.example.com",
	}

	err := k.AddChainConfig(ctx, config)
	suite.Require().NoError(err)

	// Set environment variable for RPC override
	overrideRPC := "https://override-ethereum-rpc.example.com"
	os.Setenv("UTV_CHAIN_RPC_1", overrideRPC)
	defer os.Unsetenv("UTV_CHAIN_RPC_1")

	// Get the chain config with RPC override
	retrievedConfig, err := k.GetChainConfigWithRPCOverride(ctx, config.ChainId)
	suite.Require().NoError(err)
	suite.Require().Equal(config.ChainId, retrievedConfig.ChainId)
	suite.Require().Equal(config.ChainName, retrievedConfig.ChainName)
	suite.Require().Equal(config.CaipPrefix, retrievedConfig.CaipPrefix)
	suite.Require().Equal(config.LockerContractAddress, retrievedConfig.LockerContractAddress)
	suite.Require().Equal(config.UsdcAddress, retrievedConfig.UsdcAddress)
	suite.Require().Equal(overrideRPC, retrievedConfig.PublicRpcUrl, "RPC URL should be overridden by environment variable")

	// Test getting a non-existent chain config with RPC override
	_, err = k.GetChainConfigWithRPCOverride(ctx, "non-existent-chain")
	suite.Require().Error(err, "Getting a non-existent chain config should fail")
}

func (suite *KeeperTestSuite) TestGetAllChainConfigs() {
	k := suite.fixture.k
	ctx := suite.fixture.ctx

	// Create and add multiple test chain configs
	configs := []types.ChainConfigData{
		{
			ChainId:               "1",
			ChainName:             "Ethereum Mainnet",
			CaipPrefix:            "eip155:1",
			LockerContractAddress: "0x1234567890AbCdEf1234567890AbCdEf12345678",
			UsdcAddress:           "0xabcdef1234567890AbCdEf1234567890AbCdEf12",
			PublicRpcUrl:          "https://ethereum-rpc.example.com",
		},
		{
			ChainId:               "137",
			ChainName:             "Polygon Mainnet",
			CaipPrefix:            "eip155:137",
			LockerContractAddress: "0x9876543210AbCdEf9876543210AbCdEf98765432",
			UsdcAddress:           "0xfedcba9876543210FeDcBa9876543210FeDcBa98",
			PublicRpcUrl:          "https://polygon-rpc.example.com",
		},
		{
			ChainId:               "10",
			ChainName:             "Optimism Mainnet",
			CaipPrefix:            "eip155:10",
			LockerContractAddress: "0x1111222233334444555566667777888899990000",
			UsdcAddress:           "0xaaabbbcccdddeeefff1111222233334444555566",
			PublicRpcUrl:          "https://optimism-rpc.example.com",
		},
	}

	for _, config := range configs {
		err := k.AddChainConfig(ctx, config)
		suite.Require().NoError(err)
	}

	// Get all chain configs
	retrievedConfigs, err := k.GetAllChainConfigs(ctx)
	suite.Require().NoError(err)
	suite.Require().Len(retrievedConfigs, len(configs), "Should retrieve all added chain configs")

	// Create a map of expected chain IDs
	expectedChainIDs := make(map[string]bool)
	for _, config := range configs {
		expectedChainIDs[config.ChainId] = true
	}

	// Check if all expected chain IDs are present in the retrieved configs
	for _, config := range retrievedConfigs {
		suite.Require().True(expectedChainIDs[config.ChainId], "Retrieved chain config should be in the expected set")
	}
}

func (suite *KeeperTestSuite) TestGenesisImportExport() {
	k := suite.fixture.k
	ctx := suite.fixture.ctx

	// Create and add multiple test chain configs
	configs := []types.ChainConfigData{
		{
			ChainId:               "1",
			ChainName:             "Ethereum Mainnet",
			CaipPrefix:            "eip155:1",
			LockerContractAddress: "0x1234567890AbCdEf1234567890AbCdEf12345678",
			UsdcAddress:           "0xabcdef1234567890AbCdEf1234567890AbCdEf12",
			PublicRpcUrl:          "https://ethereum-rpc.example.com",
		},
		{
			ChainId:               "137",
			ChainName:             "Polygon Mainnet",
			CaipPrefix:            "eip155:137",
			LockerContractAddress: "0x9876543210AbCdEf9876543210AbCdEf98765432",
			UsdcAddress:           "0xfedcba9876543210FeDcBa9876543210FeDcBa98",
			PublicRpcUrl:          "https://polygon-rpc.example.com",
		},
	}

	// Add the configs to the keeper
	for _, config := range configs {
		err := k.AddChainConfig(ctx, config)
		suite.Require().NoError(err)
	}

	// Set some custom params
	params := types.Params{
		SomeValue: true,
	}
	err := k.Params.Set(ctx, params)
	suite.Require().NoError(err)

	// Export genesis
	exportedGenesis := k.ExportGenesis(ctx)
	suite.Require().NotNil(exportedGenesis)
	suite.Require().Equal(params.SomeValue, exportedGenesis.Params.SomeValue, "Exported params should match")
	suite.Require().Len(exportedGenesis.ChainConfigs, len(configs), "Exported genesis should contain all chain configs")

	// Create a new keeper for import testing
	newFixture := SetupTest(suite.T())
	newK := newFixture.k
	newCtx := newFixture.ctx

	// Import genesis into the new keeper
	err = newK.InitGenesis(newCtx, exportedGenesis)
	suite.Require().NoError(err)

	// Check if the params were imported correctly
	importedParams, err := newK.Params.Get(newCtx)
	suite.Require().NoError(err)
	suite.Require().Equal(params.SomeValue, importedParams.SomeValue, "Imported params should match the exported ones")

	// Check if the chain configs were imported correctly
	importedConfigs, err := newK.GetAllChainConfigs(newCtx)
	suite.Require().NoError(err)
	suite.Require().Len(importedConfigs, len(configs), "Imported genesis should contain all chain configs")

	// Create a map of expected chain IDs
	expectedChainIDs := make(map[string]bool)
	for _, config := range configs {
		expectedChainIDs[config.ChainId] = true
	}

	// Check if all expected chain IDs are present in the imported configs
	for _, config := range importedConfigs {
		suite.Require().True(expectedChainIDs[config.ChainId], "Imported chain config should be in the expected set")
	}
}

func (suite *KeeperTestSuite) TestInvalidChainConfig() {
	k := suite.fixture.k
	ctx := suite.fixture.ctx

	testCases := []struct {
		name   string
		config types.ChainConfigData
		errMsg string
	}{
		{
			name: "Empty chain ID",
			config: types.ChainConfigData{
				ChainId:               "",
				ChainName:             "Test Chain",
				CaipPrefix:            "eip155:1",
				LockerContractAddress: "0x1234",
				UsdcAddress:           "0x5678",
				PublicRpcUrl:          "https://rpc.example.com",
			},
			errMsg: "chain ID cannot be empty",
		},
		{
			name: "Empty chain name",
			config: types.ChainConfigData{
				ChainId:               "1",
				ChainName:             "",
				CaipPrefix:            "eip155:1",
				LockerContractAddress: "0x1234",
				UsdcAddress:           "0x5678",
				PublicRpcUrl:          "https://rpc.example.com",
			},
			errMsg: "chain name cannot be empty",
		},
		{
			name: "Invalid CAIP prefix",
			config: types.ChainConfigData{
				ChainId:               "1",
				ChainName:             "Test Chain",
				CaipPrefix:            "invalid",
				LockerContractAddress: "0x1234",
				UsdcAddress:           "0x5678",
				PublicRpcUrl:          "https://rpc.example.com",
			},
			errMsg: "invalid CAIP prefix format",
		},
		{
			name: "Empty locker contract address",
			config: types.ChainConfigData{
				ChainId:               "1",
				ChainName:             "Test Chain",
				CaipPrefix:            "eip155:1",
				LockerContractAddress: "",
				UsdcAddress:           "0x5678",
				PublicRpcUrl:          "https://rpc.example.com",
			},
			errMsg: "locker contract address cannot be empty",
		},
		{
			name: "Empty USDC address",
			config: types.ChainConfigData{
				ChainId:               "1",
				ChainName:             "Test Chain",
				CaipPrefix:            "eip155:1",
				LockerContractAddress: "0x1234",
				UsdcAddress:           "",
				PublicRpcUrl:          "https://rpc.example.com",
			},
			errMsg: "USDC address cannot be empty",
		},
		{
			name: "Empty RPC URL",
			config: types.ChainConfigData{
				ChainId:               "1",
				ChainName:             "Test Chain",
				CaipPrefix:            "eip155:1",
				LockerContractAddress: "0x1234",
				UsdcAddress:           "0x5678",
				PublicRpcUrl:          "",
			},
			errMsg: "public RPC URL cannot be empty",
		},
	}

	for _, tc := range testCases {
		suite.Run(tc.name, func() {
			err := k.AddChainConfig(ctx, tc.config)
			suite.Require().Error(err)
			suite.Require().Contains(err.Error(), tc.errMsg)
		})
	}
}
