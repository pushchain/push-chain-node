package keeper_test

import (
	"testing"

	"cosmossdk.io/collections"
	"cosmossdk.io/log"
	storetypes "cosmossdk.io/store/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/cosmos/cosmos-sdk/codec"
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
// (PRC20 reverse index) and the migration that rebuilds it for pre-upgrade
// state.

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

// seedLegacyTokens writes TokenConfigs through a parallel plain collections.Map
// at the same storage prefix, bypassing the IndexedMap framework. Simulates
// pre-upgrade state where TokenConfigs has entries but PRC20Index is empty —
// exactly the testnet upgrade scenario. Mirrors the v3 migration test pattern.
func seedLegacyTokens(t *testing.T, ctx sdk.Context, k *keeper.Keeper, cdc codec.BinaryCodec, cfgs []types.TokenConfig) {
	t.Helper()
	legacyMap := collections.NewMap(
		k.SchemaBuilder(),
		types.TokenConfigsKey,
		types.TokenConfigsName,
		collections.StringKey,
		codec.CollValue[types.TokenConfig](cdc),
	)
	for _, cfg := range cfgs {
		key := types.GetTokenConfigsStorageKey(cfg.Chain, cfg.Address)
		require.NoError(t, legacyMap.Set(ctx, key, cfg))
	}
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

// TestRebuildPRC20Index_FillsGapForLegacyEntries is THE migration test.
// Simulates the testnet upgrade scenario: writes TokenConfigs through a
// parallel plain Map (matching what an old node wrote pre-upgrade, when no
// PRC20Index existed). Verifies GetTokenConfigByPRC20 returns NotFound for
// those legacy entries. Then runs RebuildPRC20Index. Verifies every PRC20-
// bearing entry is now resolvable.
func TestRebuildPRC20Index_FillsGapForLegacyEntries(t *testing.T) {
	ctx, k, encCfg := setupPRC20Keeper(t)

	legacy := []types.TokenConfig{
		makeTokenCfg(tcChainA, "0xUSDC_eth", "0xPRC20_eth_usdc"),
		makeTokenCfg(tcChainA, "0xUSDT_eth", "0xPRC20_eth_usdt"),
		makeTokenCfg(tcChainB, "0xUSDC_pol", "0xPRC20_pol_usdc"),
		makeTokenCfg(tcChainC, "USDCsvm", "0xPRC20_svm_usdc"),
		makeTokenCfg(tcChainA, "0xNativeOnly", ""), // no PRC20 — never indexed
	}

	// Write directly to the underlying primary map at the TokenConfigs prefix,
	// bypassing the IndexedMap framework. This produces the exact storage
	// state a pre-upgrade node would have.
	seedLegacyTokens(t, ctx, &k, encCfg.Codec, legacy)

	// Pre-rebuild: TokenConfigs.Get works (reads from primary store), but
	// GetTokenConfigByPRC20 returns NotFound because PRC20Index is empty.
	cfg, err := k.GetTokenConfig(ctx, tcChainA, "0xUSDC_eth")
	require.NoError(t, err, "primary store has the row")
	require.Equal(t, "0xPRC20_eth_usdc", cfg.NativeRepresentation.ContractAddress)

	_, err = k.GetTokenConfigByPRC20(ctx, tcChainA, "0xPRC20_eth_usdc")
	require.ErrorIs(t, err, collections.ErrNotFound,
		"pre-rebuild: PRC20Index is empty even though TokenConfigs has the row")

	// Run the migration.
	require.NoError(t, k.RebuildPRC20Index(ctx))

	// Post-rebuild: every PRC20-bearing legacy entry is now resolvable.
	for _, cfg := range legacy {
		if cfg.NativeRepresentation == nil {
			// No-NativeRepresentation tokens stay unreachable by PRC20 — by design.
			continue
		}
		got, err := k.GetTokenConfigByPRC20(ctx, cfg.Chain, cfg.NativeRepresentation.ContractAddress)
		require.NoError(t, err,
			"PRC20 %q on chain %q should resolve after RebuildPRC20Index",
			cfg.NativeRepresentation.ContractAddress, cfg.Chain)
		require.Equal(t, cfg.Address, got.Address)
	}
}

// TestRebuildPRC20Index_IsIdempotent confirms re-running the migration on
// already-populated state is a safe no-op.
func TestRebuildPRC20Index_IsIdempotent(t *testing.T) {
	ctx, k, _ := setupPRC20Keeper(t)

	require.NoError(t, k.TokenConfigs.Set(ctx,
		types.GetTokenConfigsStorageKey(tcChainA, "0xUSDC"),
		makeTokenCfg(tcChainA, "0xUSDC", "0xPRC20")))

	require.NoError(t, k.RebuildPRC20Index(ctx))
	require.NoError(t, k.RebuildPRC20Index(ctx))

	got, err := k.GetTokenConfigByPRC20(ctx, tcChainA, "0xPRC20")
	require.NoError(t, err)
	require.Equal(t, "0xUSDC", got.Address)
}

// TestRebuildPRC20Index_EmptyStateIsNoOp confirms the migration handles a
// fresh chain with no tokens registered.
func TestRebuildPRC20Index_EmptyStateIsNoOp(t *testing.T) {
	ctx, k, _ := setupPRC20Keeper(t)
	require.NoError(t, k.RebuildPRC20Index(ctx))
}
