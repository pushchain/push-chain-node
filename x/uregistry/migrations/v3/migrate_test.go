package v3_test

import (
	"testing"
	"time"

	"cosmossdk.io/collections"
	"cosmossdk.io/log"
	storetypes "cosmossdk.io/store/types"

	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	sdkaddress "github.com/cosmos/cosmos-sdk/codec/address"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/testutil/integration"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	evmkeeper "github.com/cosmos/evm/x/vm/keeper"
	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/app"
	"github.com/pushchain/push-chain-node/x/uregistry/keeper"
	v3 "github.com/pushchain/push-chain-node/x/uregistry/migrations/v3"
	"github.com/pushchain/push-chain-node/x/uregistry/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// legacyChainConfig mirrors ChainConfig as it existed before v3 —
// fields 9 (vault_address) and 10 (vault_methods) are absent.
// The proto field tags are identical to the generated types.pb.go so that
// marshaling produces the exact binary encoding an old node would have written.
type legacyChainConfig struct {
	Chain                  string                    `protobuf:"bytes,1,opt,name=chain,proto3" json:"chain,omitempty"`
	VmType                 types.VmType              `protobuf:"varint,2,opt,name=vm_type,json=vmType,proto3,enum=uregistry.v1.VmType" json:"vm_type,omitempty"`
	PublicRpcUrl           string                    `protobuf:"bytes,3,opt,name=public_rpc_url,json=publicRpcUrl,proto3" json:"public_rpc_url,omitempty"`
	GatewayAddress         string                    `protobuf:"bytes,4,opt,name=gateway_address,json=gatewayAddress,proto3" json:"gateway_address,omitempty"`
	BlockConfirmation      *types.BlockConfirmation  `protobuf:"bytes,5,opt,name=block_confirmation,json=blockConfirmation,proto3" json:"block_confirmation,omitempty"`
	GatewayMethods         []*types.GatewayMethods   `protobuf:"bytes,6,rep,name=gateway_methods,json=gatewayMethods,proto3" json:"gateway_methods,omitempty"`
	Enabled                *types.ChainEnabled       `protobuf:"bytes,7,opt,name=enabled,proto3" json:"enabled,omitempty"`
	GasOracleFetchInterval time.Duration             `protobuf:"bytes,8,opt,name=gas_oracle_fetch_interval,json=gasOracleFetchInterval,proto3,stdduration" json:"gas_oracle_fetch_interval"`
	// fields 9 (vault_address) and 10 (vault_methods) deliberately absent
}

func (m *legacyChainConfig) Reset()         {}
func (m *legacyChainConfig) String() string { return m.Chain }
func (m *legacyChainConfig) ProtoMessage()  {}

func setupKeeper(t *testing.T) (sdk.Context, keeper.Keeper, moduletestutil.TestEncodingConfig) {
	t.Helper()

	cfg := sdk.GetConfig()
	cfg.SetBech32PrefixForAccount(app.Bech32PrefixAccAddr, app.Bech32PrefixAccPub)
	cfg.SetBech32PrefixForValidator(app.Bech32PrefixValAddr, app.Bech32PrefixValPub)
	cfg.SetBech32PrefixForConsensusNode(app.Bech32PrefixConsAddr, app.Bech32PrefixConsPub)
	cfg.SetCoinType(app.CoinType)

	_ = sdkaddress.NewBech32Codec(app.Bech32PrefixAccAddr)

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

// seedLegacyConfigs writes old-format chain configs (no vault fields) directly
// into the store using a collections.Map typed to legacyChainConfig.
// This produces the exact same binary encoding an old node would have written.
func seedLegacyConfigs(t *testing.T, ctx sdk.Context, k *keeper.Keeper, cdc codec.BinaryCodec, cfgs []legacyChainConfig) {
	t.Helper()

	legacyMap := collections.NewMap(
		k.SchemaBuilder(),
		types.ChainConfigsKey,
		types.ChainConfigsName,
		collections.StringKey,
		codec.CollValue[legacyChainConfig](cdc),
	)

	for _, cfg := range cfgs {
		require.NoError(t, legacyMap.Set(ctx, cfg.Chain, cfg))
	}
}

// TestMigrateChainConfigs_V3_OldData seeds chain configs encoded in the pre-v3
// binary format (no vault fields) and verifies that:
//  1. The migration completes without error.
//  2. All pre-existing fields survive unchanged.
//  3. vault_address and vault_methods are empty after migration.
func TestMigrateChainConfigs_V3_OldData(t *testing.T) {
	ctx, k, encCfg := setupKeeper(t)
	logger := log.NewTestLogger(t)

	oldConfigs := []legacyChainConfig{
		{
			Chain:          "eip155:11155111",
			VmType:         types.VmType_EVM,
			PublicRpcUrl:   "https://eth-sepolia.public.blastapi.io",
			GatewayAddress: "0x05bD7a3D18324c1F7e216f7fBF2b15985aE5281A",
			BlockConfirmation: &types.BlockConfirmation{
				FastInbound:     0,
				StandardInbound: 1,
			},
			GatewayMethods: []*types.GatewayMethods{
				{
					Name:             "sendFunds",
					Identifier:       "0x65f4dbe1",
					EventIdentifier:  "0x33e6cf63a9ddbaee9d86893573e2616fe7a78fc9b7b23acb7da8b58bd0024041",
					ConfirmationType: types.ConfirmationType_CONFIRMATION_TYPE_STANDARD,
				},
			},
			Enabled: &types.ChainEnabled{
				IsInboundEnabled:  true,
				IsOutboundEnabled: true,
			},
			GasOracleFetchInterval: 30 * time.Second,
		},
		{
			Chain:          "solana:EtWTRABZaYq6iMfeYKouRu166VU2xqa1",
			VmType:         types.VmType_SVM,
			PublicRpcUrl:   "https://api.devnet.solana.com",
			GatewayAddress: "CFVSincHYbETh2k7w6u1ENEkjbSLtveRCEBupKidw2VS",
			BlockConfirmation: &types.BlockConfirmation{
				FastInbound:     0,
				StandardInbound: 1,
			},
			GatewayMethods: []*types.GatewayMethods{
				{
					Name:             "add_funds",
					Identifier:       "84ed4c39500ab38a",
					EventIdentifier:  "7f1f6cffbb134644",
					ConfirmationType: types.ConfirmationType_CONFIRMATION_TYPE_FAST,
				},
			},
			Enabled: &types.ChainEnabled{
				IsInboundEnabled:  true,
				IsOutboundEnabled: false,
			},
			GasOracleFetchInterval: 30 * time.Second,
		},
	}

	seedLegacyConfigs(t, ctx, &k, encCfg.Codec, oldConfigs)

	// Run the v3 migration against old-format data.
	err := v3.MigrateChainConfigs(ctx, &k, encCfg.Codec, logger)
	require.NoError(t, err)

	// Verify each config is readable by the new type and vault fields are absent.
	for _, old := range oldConfigs {
		got, err := k.ChainConfigs.Get(ctx, old.Chain)
		require.NoError(t, err, "chain %s should exist after migration", old.Chain)

		// Pre-existing fields must be unchanged.
		require.Equal(t, old.Chain, got.Chain)
		require.Equal(t, old.VmType, got.VmType)
		require.Equal(t, old.PublicRpcUrl, got.PublicRpcUrl)
		require.Equal(t, old.GatewayAddress, got.GatewayAddress)
		require.Equal(t, old.GasOracleFetchInterval, got.GasOracleFetchInterval)
		require.Equal(t, len(old.GatewayMethods), len(got.GatewayMethods))
		for i, gm := range old.GatewayMethods {
			require.Equal(t, gm.Name, got.GatewayMethods[i].Name)
			require.Equal(t, gm.Identifier, got.GatewayMethods[i].Identifier)
			require.Equal(t, gm.EventIdentifier, got.GatewayMethods[i].EventIdentifier)
			require.Equal(t, gm.ConfirmationType, got.GatewayMethods[i].ConfirmationType)
		}

		// vault fields must be zero-value — they were never in the old encoding.
		require.Empty(t, got.VaultAddress, "vault_address should be empty for pre-v3 data")
		require.Empty(t, got.VaultMethods, "vault_methods should be empty for pre-v3 data")
	}
}

// TestMigrateChainConfigs_V3_EmptyStore verifies the migration succeeds when
// there are no chain configs in the store (fresh chain or first-time setup).
func TestMigrateChainConfigs_V3_EmptyStore(t *testing.T) {
	ctx, k, encCfg := setupKeeper(t)
	logger := log.NewTestLogger(t)

	err := v3.MigrateChainConfigs(ctx, &k, encCfg.Codec, logger)
	require.NoError(t, err)

	// Store must still be empty after migration.
	iter, err := k.ChainConfigs.Iterate(ctx, nil)
	require.NoError(t, err)
	defer iter.Close()
	require.False(t, iter.Valid(), "store should remain empty after migrating empty state")
}

// TestMigrateChainConfigs_V3_PreservesNewFields verifies that a config already
// written with the new schema (vault fields present) is not corrupted by the migration.
func TestMigrateChainConfigs_V3_PreservesNewFields(t *testing.T) {
	ctx, k, encCfg := setupKeeper(t)
	logger := log.NewTestLogger(t)

	cfg := types.ChainConfig{
		Chain:          "eip155:97",
		VmType:         types.VmType_EVM,
		PublicRpcUrl:   "https://bsc-testnet-rpc.publicnode.com",
		GatewayAddress: "0x44aFFC61983F4348DdddB886349eb992C061EaC0",
		BlockConfirmation: &types.BlockConfirmation{
			FastInbound:     0,
			StandardInbound: 1,
		},
		GasOracleFetchInterval: 30 * time.Second,
		VaultAddress:           "0xVaultAddr",
		VaultMethods: []*types.VaultMethods{
			{
				Name:             "deposit",
				Identifier:       "0xb6b55f25",
				EventIdentifier:  "0x3c4e6c56cc5f2c26c92b91ee2f8bdc4e844b407bd1402b34ac1ef1f875d3c4b5",
				ConfirmationType: types.ConfirmationType_CONFIRMATION_TYPE_STANDARD,
			},
		},
	}
	require.NoError(t, k.ChainConfigs.Set(ctx, cfg.Chain, cfg))

	err := v3.MigrateChainConfigs(ctx, &k, encCfg.Codec, logger)
	require.NoError(t, err)

	got, err := k.ChainConfigs.Get(ctx, cfg.Chain)
	require.NoError(t, err)

	require.Equal(t, cfg.VaultAddress, got.VaultAddress)
	require.Len(t, got.VaultMethods, 1)
	require.Equal(t, cfg.VaultMethods[0].Name, got.VaultMethods[0].Name)
	require.Equal(t, cfg.VaultMethods[0].Identifier, got.VaultMethods[0].Identifier)
}
