package keeper_test

import (
	"context"
	"math/big"
	"testing"

	"github.com/golang/mock/gomock"

	"github.com/ethereum/go-ethereum/common"
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

	feemarketkeeper "github.com/cosmos/evm/x/feemarket/keeper"
	"github.com/cosmos/evm/x/vm/statedb"
	"github.com/rollchains/pchain/app"
	module "github.com/rollchains/pchain/x/ue"
	"github.com/rollchains/pchain/x/ue/keeper"
	"github.com/rollchains/pchain/x/ue/mocks"
	"github.com/rollchains/pchain/x/ue/types"

	evmtypes "github.com/cosmos/evm/x/vm/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	uvalidatorKeeper "github.com/rollchains/pchain/x/uvalidator/keeper"
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
	evmAddrs   []common.Address

	ctrl                *gomock.Controller
	mockBankKeeper      *mocks.MockBankKeeper
	mockUTVKeeper       *mocks.MockUtvKeeper
	mockEVMKeeper       *mocks.MockEVMKeeper
	mockUregistryKeeper *mocks.MockUregistryKeeper
}

func SetupTest(t *testing.T) *testFixture {
	t.Helper()
	f := new(testFixture)

	f.ctrl = gomock.NewController(t)
	t.Cleanup(f.ctrl.Finish)

	f.mockBankKeeper = mocks.NewMockBankKeeper(f.ctrl)
	f.mockUTVKeeper = mocks.NewMockUtvKeeper(f.ctrl)
	f.mockEVMKeeper = mocks.NewMockEVMKeeper(f.ctrl)
	f.mockUregistryKeeper = mocks.NewMockUregistryKeeper(f.ctrl)

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

	evmAddrs := make([]common.Address, len(f.addrs))
	for i, addr := range f.addrs {
		evmAddrs[i] = common.BytesToAddress(addr.Bytes())
	}
	f.evmAddrs = evmAddrs

	keys := storetypes.NewKVStoreKeys(authtypes.ModuleName, banktypes.ModuleName, stakingtypes.ModuleName, minttypes.ModuleName, types.ModuleName)
	f.ctx = sdk.NewContext(integration.CreateMultiStore(keys, logger), cmtproto.Header{}, false, logger)

	// Register SDK modules.
	registerBaseSDKModules(logger, f, encCfg, keys, accountAddressCodec, validatorAddressCodec, consensusAddressCodec)

	// Setup Keeper.
	f.k = keeper.NewKeeper(encCfg.Codec, runtime.NewKVStoreService(keys[types.ModuleName]), logger, f.govModAddr, f.mockEVMKeeper, &feemarketkeeper.Keeper{}, f.mockBankKeeper, authkeeper.AccountKeeper{}, f.mockUregistryKeeper, f.mockUTVKeeper, &uvalidatorKeeper.Keeper{})
	f.msgServer = keeper.NewMsgServerImpl(f.k)
	f.queryServer = keeper.NewQuerier(f.k)
	f.appModule = module.NewAppModule(encCfg.Codec, f.k, f.mockEVMKeeper, &feemarketkeeper.Keeper{}, f.mockBankKeeper, authkeeper.AccountKeeper{}, f.mockUregistryKeeper, f.mockUTVKeeper, &uvalidatorKeeper.Keeper{})

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

// MockEVMKeeper implements only the methods used by your `InitGenesis` function.
type MockEVMKeeper struct{}

func (m MockEVMKeeper) SetAccount(ctx sdk.Context, addr common.Address, account statedb.Account) error {
	// no-op mock
	return nil
}

func (m MockEVMKeeper) SetCode(ctx sdk.Context, codeHash, code []byte) {
	// no-op mock
}

func (m MockEVMKeeper) SetState(ctx sdk.Context, addr common.Address, key common.Hash, value []byte) {
	// no-op mock
}

func (m MockEVMKeeper) CallEVM(
	ctx sdk.Context,
	abi abi.ABI,
	from, contract common.Address,
	commit bool,
	method string,
	args ...interface{},
) (*evmtypes.MsgEthereumTxResponse, error) {

	addr := common.HexToAddress("0x1234567890abcdef1234567890abcdef12345678")

	// ABI‑style left‑pad to 32 bytes
	padded := common.LeftPadBytes(addr.Bytes(), 32)
	return &evmtypes.MsgEthereumTxResponse{
		Ret: padded, // flag : need to correct his mock for MintPC test
	}, nil
}

type MockUTVKeeper struct{}

func (m *MockUTVKeeper) VerifyGatewayInteractionTx(ctx context.Context, owner string, txHash string, chain string) error {
	return nil // simulate a pass-through
}

func (m *MockUTVKeeper) VerifyAndGetLockedFunds(ctx context.Context, ownerKey, txHash, chain string) (big.Int, uint32, error) {
	return *big.NewInt(0), 0, nil // simulate a pass-through
}

type MockBankKeeper struct{}

func (m MockBankKeeper) MintCoins(ctx context.Context, moduleName string, amt sdk.Coins) error {
	return nil
}
func (m MockBankKeeper) SendCoinsFromAccountToModule(ctx context.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error {
	return nil
}

func (m MockBankKeeper) SendCoinsFromModuleToAccount(ctx context.Context, senderAddr string, recipientAddr sdk.AccAddress, amt sdk.Coins) error {
	return nil
}
func (m MockBankKeeper) BurnCoins(ctx context.Context, moduleName string, amt sdk.Coins) error {
	return nil
}
