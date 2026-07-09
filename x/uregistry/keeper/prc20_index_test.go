package keeper_test

import (
	"testing"

	"cosmossdk.io/collections"
	"cosmossdk.io/log"
	storetypes "cosmossdk.io/store/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/testutil/integration"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	evmkeeper "github.com/cosmos/evm/x/vm/keeper"
	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/pushchain/push-chain-node/app"
	"github.com/pushchain/push-chain-node/x/uregistry/keeper"
	"github.com/pushchain/push-chain-node/x/uregistry/types"
)

// F-2026-17035 regression suite for the IndexedMap-backed TokenConfigs
// (PRC20 reverse index).

const (
	tcChainA = "eip155:1"
	tcChainB = "eip155:137"
	tcChainC = "solana:mainnet"
)

func setupPRC20Keeper(t *testing.T) (sdk.Context, keeper.Keeper, moduletestutil.TestEncodingConfig) {
	t.Helper()

	cfg := sdk.GetConfig()
	cfg.SetBech32PrefixForAccount(app.Bech32PrefixAccAddr, app.Bech32PrefixAccPub)
	cfg.SetBech32PrefixForValidator(app.Bech32PrefixValAddr, app.Bech32PrefixValPub)
	cfg.SetBech32PrefixForConsensusNode(app.Bech32PrefixConsAddr, app.Bech32PrefixConsPub)
	cfg.SetCoinType(app.CoinType)

	logger := log.NewTestLogger(t)
	encCfg := moduletestutil.MakeTestEncodingConfig()
	types.RegisterInterfaces(encCfg.InterfaceRegistry)

	keys := storetypes.NewKVStoreKeys(types.ModuleName)
	ctx := sdk.NewContext(integration.CreateMultiStore(keys, logger), cmtproto.Header{}, false, logger)

	govAddr := authtypes.NewModuleAddress(govtypes.ModuleName).String()
	k := keeper.NewKeeper(
		encCfg.Codec,
		runtime.NewKVStoreService(keys[types.ModuleName]),
		logger,
		govAddr,
		&evmkeeper.Keeper{},
	)

	return ctx, k, encCfg
}

func makeTokenCfg(chain, address, prc20 string) types.TokenConfig {
	cfg := types.TokenConfig{
		Chain:   chain,
		Address: address,
		Name:    "TestToken",
		Symbol:  "TT",
	}
	if prc20 != "" {
		cfg.NativeRepresentation = &types.NativeRepresentation{
			ContractAddress: prc20,
		}
	}
	return cfg
}

// TestGetTokenConfigByPRC20_LookupViaIndex covers the happy path: the
// IndexedMap auto-populates PRC20Index on every Set, so lookup returns the
// matching config in O(1) without scanning all of TokenConfigs.
func TestGetTokenConfigByPRC20_LookupViaIndex(t *testing.T) {
	ctx, k, _ := setupPRC20Keeper(t)

	cfgA := makeTokenCfg(tcChainA, "0xUSDC_eth", "0xPRC20_aaa")
	cfgB := makeTokenCfg(tcChainB, "0xUSDC_polygon", "0xPRC20_bbb")
	cfgC := makeTokenCfg(tcChainC, "USDCsvm", "0xPRC20_ccc")
	for _, cfg := range []types.TokenConfig{cfgA, cfgB, cfgC} {
		require.NoError(t, k.TokenConfigs.Set(ctx, types.GetTokenConfigsStorageKey(cfg.Chain, cfg.Address), cfg))
	}

	got, err := k.GetTokenConfigByPRC20(ctx, tcChainA, "0xPRC20_aaa")
	require.NoError(t, err)
	require.Equal(t, cfgA.Address, got.Address)

	got, err = k.GetTokenConfigByPRC20(ctx, tcChainB, "0xPRC20_bbb")
	require.NoError(t, err)
	require.Equal(t, cfgB.Address, got.Address)

	got, err = k.GetTokenConfigByPRC20(ctx, tcChainC, "0xPRC20_ccc")
	require.NoError(t, err)
	require.Equal(t, cfgC.Address, got.Address)
}

// TestGetTokenConfigByPRC20_WrongChainReturnsNotFound: the (chain, prc20)
// tuple still has to match — looking up a PRC20 with the wrong chain returns
// ErrNotFound, preserving the prior caller contract.
func TestGetTokenConfigByPRC20_WrongChainReturnsNotFound(t *testing.T) {
	ctx, k, _ := setupPRC20Keeper(t)

	cfg := makeTokenCfg(tcChainA, "0xUSDC", "0xPRC20")
	require.NoError(t, k.TokenConfigs.Set(ctx, types.GetTokenConfigsStorageKey(cfg.Chain, cfg.Address), cfg))

	_, err := k.GetTokenConfigByPRC20(ctx, tcChainB, "0xPRC20")
	require.ErrorIs(t, err, collections.ErrNotFound)
}

// TestGetTokenConfigByPRC20_CaseInsensitive: PRC20 addresses are normalised
// to lowercase at both Set time (via the index function) and Get time, so
// arbitrary case + whitespace in queries hits the same row.
func TestGetTokenConfigByPRC20_CaseInsensitive(t *testing.T) {
	ctx, k, _ := setupPRC20Keeper(t)

	cfg := makeTokenCfg(tcChainA, "0xUSDC", "0xPrc20MixedCase")
	require.NoError(t, k.TokenConfigs.Set(ctx, types.GetTokenConfigsStorageKey(cfg.Chain, cfg.Address), cfg))

	for _, query := range []string{
		"0xPrc20MixedCase",
		"0xprc20mixedcase",
		"0XPRC20MIXEDCASE",
		"  0xPrc20MixedCase  ",
	} {
		got, err := k.GetTokenConfigByPRC20(ctx, tcChainA, query)
		require.NoError(t, err, "lookup failed for %q", query)
		require.Equal(t, "0xUSDC", got.Address)
	}
}

// TestGetTokenConfigByPRC20_EmptyAddressRejected guards against empty input
// (which would otherwise collide with the sentinel the framework uses to
// index rows with no NativeRepresentation).
func TestGetTokenConfigByPRC20_EmptyAddressRejected(t *testing.T) {
	ctx, k, _ := setupPRC20Keeper(t)

	_, err := k.GetTokenConfigByPRC20(ctx, tcChainA, "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "empty")

	_, err = k.GetTokenConfigByPRC20(ctx, tcChainA, "   ")
	require.Error(t, err)
	require.Contains(t, err.Error(), "empty")
}

// TestGetTokenConfigByPRC20_NoNativeRepresentationNotReachable: tokens that
// don't have NativeRepresentation (non-PRC20 tokens) index under the empty
// sentinel and are never returned by lookup against a non-empty PRC20.
func TestGetTokenConfigByPRC20_NoNativeRepresentationNotReachable(t *testing.T) {
	ctx, k, _ := setupPRC20Keeper(t)

	// Two non-PRC20 + one PRC20.
	for _, cfg := range []types.TokenConfig{
		makeTokenCfg(tcChainA, "0xNative1", ""),
		makeTokenCfg(tcChainA, "0xNative2", ""),
		makeTokenCfg(tcChainA, "0xWrapped", "0xPRC20"),
	} {
		require.NoError(t, k.TokenConfigs.Set(ctx, types.GetTokenConfigsStorageKey(cfg.Chain, cfg.Address), cfg))
	}

	// Only the PRC20-bearing token is reachable by the index.
	got, err := k.GetTokenConfigByPRC20(ctx, tcChainA, "0xPRC20")
	require.NoError(t, err)
	require.Equal(t, "0xWrapped", got.Address)
}

// TestGetTokenConfigByPRC20_UpdateRemovesOldIndexEntry: changing a
// TokenConfig's PRC20 address must remove the OLD refKey from the index
// (otherwise the old PRC20 would still resolve to the now-stale row).
// This is the load-bearing test for IndexedMap.Reference's update path.
func TestGetTokenConfigByPRC20_UpdateRemovesOldIndexEntry(t *testing.T) {
	ctx, k, _ := setupPRC20Keeper(t)

	key := types.GetTokenConfigsStorageKey(tcChainA, "0xUSDC")
	require.NoError(t, k.TokenConfigs.Set(ctx, key, makeTokenCfg(tcChainA, "0xUSDC", "0xOldPRC20")))

	got, err := k.GetTokenConfigByPRC20(ctx, tcChainA, "0xOldPRC20")
	require.NoError(t, err)
	require.Equal(t, "0xUSDC", got.Address)

	// Update to a different PRC20.
	require.NoError(t, k.TokenConfigs.Set(ctx, key, makeTokenCfg(tcChainA, "0xUSDC", "0xNewPRC20")))

	_, err = k.GetTokenConfigByPRC20(ctx, tcChainA, "0xOldPRC20")
	require.ErrorIs(t, err, collections.ErrNotFound, "old refKey must be dropped on update")

	got, err = k.GetTokenConfigByPRC20(ctx, tcChainA, "0xNewPRC20")
	require.NoError(t, err)
	require.Equal(t, "0xUSDC", got.Address)
}

// TestGetTokenConfigByPRC20_RemoveDropsIndexEntry: removing a TokenConfig
// must drop its PRC20Index entry (otherwise lookup would return a non-existent
// primary key).
func TestGetTokenConfigByPRC20_RemoveDropsIndexEntry(t *testing.T) {
	ctx, k, _ := setupPRC20Keeper(t)

	key := types.GetTokenConfigsStorageKey(tcChainA, "0xUSDC")
	require.NoError(t, k.TokenConfigs.Set(ctx, key, makeTokenCfg(tcChainA, "0xUSDC", "0xPRC20")))

	got, err := k.GetTokenConfigByPRC20(ctx, tcChainA, "0xPRC20")
	require.NoError(t, err)
	require.Equal(t, "0xUSDC", got.Address)

	require.NoError(t, k.TokenConfigs.Remove(ctx, key))

	_, err = k.GetTokenConfigByPRC20(ctx, tcChainA, "0xPRC20")
	require.ErrorIs(t, err, collections.ErrNotFound, "remove must drop the index entry")
}
