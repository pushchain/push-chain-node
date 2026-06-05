package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/x/uregistry/types"
)

// Regression suite for case-canonical token storage keys: the same logical
// EVM address in any case/prefix variant must always resolve to one row,
// while case-significant solana base58 addresses are preserved verbatim.

const (
	canonChainEVM = "eip155:11155111"
	canonChainSol = "solana:EtWTRABZaYq6iMfeYKouRu166VU2xqa1"

	// EIP-55 canonical form + variants of the same 20 bytes.
	tokenEIP55 = "0x1c7D4B196Cb0C7B01d743Fbc6116a902379C7238"
	tokenLower = "0x1c7d4b196cb0c7b01d743fbc6116a902379c7238"
	tokenUpper = "0X1C7D4B196CB0C7B01D743FBC6116A902379C7238"

	prc20EIP55 = "0x387b9C8Db60E74999aAAC5A2b7825b400F12d68E"
	prc20Lower = "0x387b9c8db60e74999aaac5a2b7825b400f12d68e"

	solMint = "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v"
)

func TestTokenStorageKey_CaseVariantsConverge(t *testing.T) {
	want := types.GetTokenConfigsStorageKey(canonChainEVM, tokenEIP55)
	for _, variant := range []string{tokenLower, tokenUpper, "  " + tokenEIP55 + "  "} {
		require.Equal(t, want, types.GetTokenConfigsStorageKey(canonChainEVM, variant),
			"variant %q must map to the canonical storage key", variant)
	}
	require.Contains(t, want, tokenEIP55, "canonical key embeds the EIP-55 form")
}

func TestTokenStorageKey_SolanaBase58Preserved(t *testing.T) {
	key := types.GetTokenConfigsStorageKey(canonChainSol, solMint)
	require.Contains(t, key, solMint, "base58 mint must be preserved byte-for-byte (case-significant)")
}

func TestGetTokenConfig_CrossCaseLookup(t *testing.T) {
	ctx, k, _ := setupPRC20Keeper(t)

	// Registered with lowercase; readable via any variant.
	cfg := makeTokenCfg(canonChainEVM, tokenLower, prc20EIP55)
	require.NoError(t, k.TokenConfigs.Set(ctx, types.GetTokenConfigsStorageKey(cfg.Chain, cfg.Address), cfg))

	for _, variant := range []string{tokenEIP55, tokenLower, tokenUpper} {
		got, err := k.GetTokenConfig(ctx, canonChainEVM, variant)
		require.NoError(t, err, "lookup with %q must hit the canonical row", variant)
		require.Equal(t, tokenLower, got.Address)
	}
}

func TestAddTokenConfig_DuplicateCaseVariantRejected(t *testing.T) {
	ctx, k, _ := setupPRC20Keeper(t)

	// AddTokenConfig requires the chain to be registered.
	require.NoError(t, k.ChainConfigs.Set(ctx, canonChainEVM, types.ChainConfig{Chain: canonChainEVM}))

	first := makeTokenCfg(canonChainEVM, tokenEIP55, prc20EIP55)
	require.NoError(t, k.AddTokenConfig(ctx, &first))

	// Same address, different case → same canonical key → duplicate.
	dup := makeTokenCfg(canonChainEVM, tokenLower, prc20EIP55)
	err := k.AddTokenConfig(ctx, &dup)
	require.Error(t, err)
	require.Contains(t, err.Error(), "already exists",
		"case-variant duplicate registration must collide on the canonical key")
}

func TestRemoveTokenConfig_CrossCaseRemoval(t *testing.T) {
	ctx, k, _ := setupPRC20Keeper(t)
	require.NoError(t, k.ChainConfigs.Set(ctx, canonChainEVM, types.ChainConfig{Chain: canonChainEVM}))

	cfg := makeTokenCfg(canonChainEVM, tokenEIP55, prc20EIP55)
	require.NoError(t, k.AddTokenConfig(ctx, &cfg))

	// Remove using the all-lowercase variant — must target the same row.
	require.NoError(t, k.RemoveTokenConfig(ctx, canonChainEVM, tokenLower))

	_, err := k.GetTokenConfig(ctx, canonChainEVM, tokenEIP55)
	require.Error(t, err, "row must be gone regardless of removal-key casing")
}

func TestGetTokenConfigByPRC20_RealHexEIP55Index(t *testing.T) {
	ctx, k, _ := setupPRC20Keeper(t)

	// Stored with lowercase PRC20 — index canonicalizes to EIP-55.
	cfg := makeTokenCfg(canonChainEVM, tokenEIP55, prc20Lower)
	require.NoError(t, k.TokenConfigs.Set(ctx, types.GetTokenConfigsStorageKey(cfg.Chain, cfg.Address), cfg))

	for _, query := range []string{prc20EIP55, prc20Lower, "0X387B9C8DB60E74999AAAC5A2B7825B400F12D68E"} {
		got, err := k.GetTokenConfigByPRC20(ctx, canonChainEVM, query)
		require.NoError(t, err, "PRC20 lookup with %q must resolve via EIP-55 index", query)
		require.Equal(t, tokenEIP55, got.Address)
	}
}
