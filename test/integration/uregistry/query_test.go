package integrationtest

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	utils "github.com/pushchain/push-chain-node/test/utils"
	uregistrykeeper "github.com/pushchain/push-chain-node/x/uregistry/keeper"
	uregistrytypes "github.com/pushchain/push-chain-node/x/uregistry/types"
)

// sampleChainConfig returns a well-formed ChainConfig for use in tests.
func sampleChainConfig(chain string) uregistrytypes.ChainConfig {
	return uregistrytypes.ChainConfig{
		Chain:          chain,
		VmType:         uregistrytypes.VmType_EVM,
		PublicRpcUrl:   "https://sepolia.drpc.org",
		GatewayAddress: "0x28E0F09bE2321c1420Dc60Ee146aACbD68B335Fe",
		BlockConfirmation: &uregistrytypes.BlockConfirmation{
			FastInbound:     5,
			StandardInbound: 12,
		},
		GatewayMethods: []*uregistrytypes.GatewayMethods{{
			Name:             "addFunds",
			Identifier:       "",
			EventIdentifier:  "0xb28f49668e7e76dc96d7aabe5b7f63fecfbd1c3574774c05e8204e749fd96fbd",
			ConfirmationType: 5,
		}},
		GasOracleFetchInterval: 60,
		Enabled: &uregistrytypes.ChainEnabled{
			IsInboundEnabled:  true,
			IsOutboundEnabled: true,
		},
	}
}

// sampleTokenConfig returns a well-formed TokenConfig for the given chain and address.
func sampleTokenConfig(chain, address, prc20Address string) uregistrytypes.TokenConfig {
	return uregistrytypes.TokenConfig{
		Chain:        chain,
		Address:      address,
		Name:         "USD Coin",
		Symbol:       "USDC",
		Decimals:     6,
		Enabled:      true,
		LiquidityCap: "1000000000000000000000000",
		TokenType:    uregistrytypes.TokenType_ERC20,
		NativeRepresentation: &uregistrytypes.NativeRepresentation{
			ContractAddress: prc20Address,
		},
	}
}

// TestQueryParams verifies that Params are stored and retrievable.
func TestQueryParams(t *testing.T) {
	chainApp, ctx, _, _ := utils.SetAppWithMultipleValidators(t, 1)

	querier := uregistrykeeper.NewQuerier(chainApp.UregistryKeeper)

	// The full test app does not run InitChain, so uregistry Params are not
	// seeded automatically. Seed an explicit admin here — DefaultParams() now
	// returns an empty Admin (production operators must set it explicitly in
	// genesis), so the query test supplies its own.
	const testAdmin = "push1negskcfqu09j5zvpk7nhvacnwyy2mafffy7r6a"
	params := uregistrytypes.Params{Admin: testAdmin}
	err := chainApp.UregistryKeeper.Params.Set(ctx, params)
	require.NoError(t, err)

	resp, err := querier.Params(sdk.WrapSDKContext(ctx), &uregistrytypes.QueryParamsRequest{})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotNil(t, resp.Params)
	require.NotEmpty(t, resp.Params.Admin)
	require.Equal(t, testAdmin, resp.Params.Admin)
}

// TestQueryChainConfig verifies that a stored chain config is returned by the
// ChainConfig query, and that querying an unknown chain returns an error.
func TestQueryChainConfig(t *testing.T) {
	chainApp, ctx, _, _ := utils.SetAppWithMultipleValidators(t, 1)
	querier := uregistrykeeper.NewQuerier(chainApp.UregistryKeeper)

	const chain = "eip155:11155111"
	cfg := sampleChainConfig(chain)

	t.Run("not found for unregistered chain", func(t *testing.T) {
		_, err := querier.ChainConfig(sdk.WrapSDKContext(ctx), &uregistrytypes.QueryChainConfigRequest{
			Chain: chain,
		})
		require.Error(t, err)
	})

	t.Run("returns stored chain config", func(t *testing.T) {
		err := chainApp.UregistryKeeper.ChainConfigs.Set(ctx, chain, cfg)
		require.NoError(t, err)

		resp, err := querier.ChainConfig(sdk.WrapSDKContext(ctx), &uregistrytypes.QueryChainConfigRequest{
			Chain: chain,
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.NotNil(t, resp.Config)
		require.Equal(t, chain, resp.Config.Chain)
		require.Equal(t, uregistrytypes.VmType_EVM, resp.Config.VmType)
		require.Equal(t, cfg.GatewayAddress, resp.Config.GatewayAddress)
		require.True(t, resp.Config.Enabled.IsInboundEnabled)
		require.True(t, resp.Config.Enabled.IsOutboundEnabled)
	})
}

// TestQueryAllChainConfigs verifies that AllChainConfigs returns all stored
// chain configs, and returns an empty list when none are present.
func TestQueryAllChainConfigs(t *testing.T) {
	chainApp, ctx, _, _ := utils.SetAppWithMultipleValidators(t, 1)
	querier := uregistrykeeper.NewQuerier(chainApp.UregistryKeeper)

	t.Run("empty when no configs registered", func(t *testing.T) {
		resp, err := querier.AllChainConfigs(sdk.WrapSDKContext(ctx), &uregistrytypes.QueryAllChainConfigsRequest{})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Empty(t, resp.Configs)
	})

	t.Run("returns all registered chain configs", func(t *testing.T) {
		chains := []string{"eip155:1", "eip155:137", "eip155:56"}
		for _, c := range chains {
			err := chainApp.UregistryKeeper.ChainConfigs.Set(ctx, c, sampleChainConfig(c))
			require.NoError(t, err)
		}

		resp, err := querier.AllChainConfigs(sdk.WrapSDKContext(ctx), &uregistrytypes.QueryAllChainConfigsRequest{})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Len(t, resp.Configs, len(chains))

		// Verify each chain is present.
		returnedChains := make(map[string]struct{}, len(resp.Configs))
		for _, c := range resp.Configs {
			returnedChains[c.Chain] = struct{}{}
		}
		for _, c := range chains {
			_, ok := returnedChains[c]
			require.True(t, ok, "expected chain %q in AllChainConfigs response", c)
		}
	})
}

// TestQueryTokenConfig verifies that a stored token config is returned by the
// TokenConfig query, and that querying an unknown token returns an error.
func TestQueryTokenConfig(t *testing.T) {
	chainApp, ctx, _, _ := utils.SetAppWithMultipleValidators(t, 1)
	querier := uregistrykeeper.NewQuerier(chainApp.UregistryKeeper)

	prc20Address := utils.GetDefaultAddresses().PRC20USDCAddr.String()
	usdcAddress := utils.GetDefaultAddresses().ExternalUSDCAddr.String()
	const chain = "eip155:11155111"

	t.Run("not found for unregistered token", func(t *testing.T) {
		_, err := querier.TokenConfig(sdk.WrapSDKContext(ctx), &uregistrytypes.QueryTokenConfigRequest{
			Chain:   chain,
			Address: usdcAddress,
		})
		require.Error(t, err)
	})

	t.Run("returns stored token config", func(t *testing.T) {
		tc := sampleTokenConfig(chain, usdcAddress, prc20Address)
		storageKey := uregistrytypes.GetTokenConfigsStorageKey(chain, usdcAddress)
		err := chainApp.UregistryKeeper.TokenConfigs.Set(ctx, storageKey, tc)
		require.NoError(t, err)

		resp, err := querier.TokenConfig(sdk.WrapSDKContext(ctx), &uregistrytypes.QueryTokenConfigRequest{
			Chain:   chain,
			Address: usdcAddress,
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.NotNil(t, resp.Config)
		require.Equal(t, chain, resp.Config.Chain)
		require.Equal(t, "USDC", resp.Config.Symbol)
		require.Equal(t, uint32(6), resp.Config.Decimals)
		require.Equal(t, prc20Address, resp.Config.NativeRepresentation.ContractAddress)
	})
}

// TestQueryAllTokenConfigs verifies that AllTokenConfigs returns all stored
// token configs across chains, and returns an empty list when none are present.
func TestQueryAllTokenConfigs(t *testing.T) {
	chainApp, ctx, _, _ := utils.SetAppWithMultipleValidators(t, 1)
	querier := uregistrykeeper.NewQuerier(chainApp.UregistryKeeper)

	t.Run("empty when no token configs registered", func(t *testing.T) {
		resp, err := querier.AllTokenConfigs(sdk.WrapSDKContext(ctx), &uregistrytypes.QueryAllTokenConfigsRequest{})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Empty(t, resp.Configs)
	})

	t.Run("returns all token configs across chains", func(t *testing.T) {
		prc20Address := utils.GetDefaultAddresses().PRC20USDCAddr.String()
		tokens := []struct {
			chain   string
			address string
		}{
			{"eip155:1", "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"},
			{"eip155:137", "0x2791Bca1f2de4661ED88A30C99A7a9449Aa84174"},
		}
		for _, tok := range tokens {
			tc := sampleTokenConfig(tok.chain, tok.address, prc20Address)
			storageKey := uregistrytypes.GetTokenConfigsStorageKey(tok.chain, tok.address)
			err := chainApp.UregistryKeeper.TokenConfigs.Set(ctx, storageKey, tc)
			require.NoError(t, err)
		}

		resp, err := querier.AllTokenConfigs(sdk.WrapSDKContext(ctx), &uregistrytypes.QueryAllTokenConfigsRequest{})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Len(t, resp.Configs, len(tokens))
	})
}

// TestQueryTokenConfigsByChain verifies that TokenConfigsByChain returns only
// the token configs belonging to the specified chain.
func TestQueryTokenConfigsByChain(t *testing.T) {
	chainApp, ctx, _, _ := utils.SetAppWithMultipleValidators(t, 1)
	querier := uregistrykeeper.NewQuerier(chainApp.UregistryKeeper)

	prc20Address := utils.GetDefaultAddresses().PRC20USDCAddr.String()

	const chainA = "eip155:1"
	const chainB = "eip155:137"

	tokensA := []string{
		"0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48",
		"0xdAC17F958D2ee523a2206206994597C13D831ec7",
	}
	tokenB := "0x2791Bca1f2de4661ED88A30C99A7a9449Aa84174"

	// Store two tokens on chainA and one on chainB.
	for _, addr := range tokensA {
		tc := sampleTokenConfig(chainA, addr, prc20Address)
		storageKey := uregistrytypes.GetTokenConfigsStorageKey(chainA, addr)
		require.NoError(t, chainApp.UregistryKeeper.TokenConfigs.Set(ctx, storageKey, tc))
	}
	tcB := sampleTokenConfig(chainB, tokenB, prc20Address)
	storageKeyB := uregistrytypes.GetTokenConfigsStorageKey(chainB, tokenB)
	require.NoError(t, chainApp.UregistryKeeper.TokenConfigs.Set(ctx, storageKeyB, tcB))

	t.Run("returns only tokens for chainA", func(t *testing.T) {
		resp, err := querier.TokenConfigsByChain(sdk.WrapSDKContext(ctx), &uregistrytypes.QueryTokenConfigsByChainRequest{
			Chain: chainA,
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Len(t, resp.Configs, len(tokensA), "should return exactly %d tokens for chainA", len(tokensA))
		for _, cfg := range resp.Configs {
			require.Equal(t, chainA, cfg.Chain, "all returned configs should belong to chainA")
		}
	})

	t.Run("returns only tokens for chainB", func(t *testing.T) {
		resp, err := querier.TokenConfigsByChain(sdk.WrapSDKContext(ctx), &uregistrytypes.QueryTokenConfigsByChainRequest{
			Chain: chainB,
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Len(t, resp.Configs, 1)
		require.Equal(t, chainB, resp.Configs[0].Chain)
	})

	t.Run("returns empty list for unknown chain", func(t *testing.T) {
		resp, err := querier.TokenConfigsByChain(sdk.WrapSDKContext(ctx), &uregistrytypes.QueryTokenConfigsByChainRequest{
			Chain: "eip155:999",
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Empty(t, resp.Configs)
	})
}
