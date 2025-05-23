package module_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"

	module "github.com/rollchains/pchain/x/crosschain"
	"github.com/rollchains/pchain/x/crosschain/types"
)

func TestAppModuleBasic(t *testing.T) {
	encCfg := moduletestutil.MakeTestEncodingConfig()
	appModule := module.AppModuleBasic{}

	t.Run("module_name", func(t *testing.T) {
		require.Equal(t, types.ModuleName, appModule.Name())
	})

	t.Run("default_genesis", func(t *testing.T) {
		genesis := appModule.DefaultGenesis(encCfg.Codec)
		require.NotNil(t, genesis)

		var genesisState types.GenesisState
		err := encCfg.Codec.UnmarshalJSON(genesis, &genesisState)
		require.NoError(t, err)

		// Verify default parameters
		require.NotNil(t, genesisState.Params)
		require.Equal(t, types.DefaultParams(), genesisState.Params)
	})

	t.Run("validate_genesis_valid", func(t *testing.T) {
		validGenesis := &types.GenesisState{
			Params: types.Params{
				Admin: "push1234567890123456789012345678901234567890",
			},
		}
		genesis, err := encCfg.Codec.MarshalJSON(validGenesis)
		require.NoError(t, err)

		err = appModule.ValidateGenesis(encCfg.Codec, nil, genesis)
		require.NoError(t, err)
	})

	t.Run("validate_genesis_invalid", func(t *testing.T) {
		invalidGenesis := &types.GenesisState{
			Params: types.Params{
				Admin: "", // Invalid empty admin
			},
		}
		genesis, err := encCfg.Codec.MarshalJSON(invalidGenesis)
		require.NoError(t, err)

		err = appModule.ValidateGenesis(encCfg.Codec, nil, genesis)
		require.Error(t, err)
		require.Contains(t, err.Error(), "admin cannot be empty")
	})
}

func TestModuleGenesisHandling(t *testing.T) {
	encCfg := moduletestutil.MakeTestEncodingConfig()

	tests := []struct {
		name          string
		genesisState  types.GenesisState
		expectError   bool
		errorContains string
	}{
		{
			name: "valid_genesis",
			genesisState: types.GenesisState{
				Params: types.Params{
					Admin: "push1234567890123456789012345678901234567890",
				},
			},
			expectError: false,
		},
		{
			name: "empty_admin",
			genesisState: types.GenesisState{
				Params: types.Params{
					Admin: "",
				},
			},
			expectError:   true,
			errorContains: "admin cannot be empty",
		},
		{
			name: "invalid_admin_format",
			genesisState: types.GenesisState{
				Params: types.Params{
					Admin: "invalid_address_format",
				},
			},
			expectError:   true,
			errorContains: "invalid bech32",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			appModule := module.AppModuleBasic{}

			genesis, err := encCfg.Codec.MarshalJSON(&tc.genesisState)
			require.NoError(t, err)

			err = appModule.ValidateGenesis(encCfg.Codec, nil, genesis)

			if tc.expectError {
				require.Error(t, err)
				if tc.errorContains != "" {
					require.Contains(t, err.Error(), tc.errorContains)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestModuleInterfaces(t *testing.T) {
	// Test AppModuleBasic interfaces
	appModuleBasic := module.AppModuleBasic{}

	t.Run("app_module_basic_interface", func(t *testing.T) {
		// Test that AppModuleBasic implements required interfaces
		require.Implements(t, (*interface{})(nil), appModuleBasic)

		// Test interface methods
		require.Equal(t, types.ModuleName, appModuleBasic.Name())
	})
}

func TestModuleCodecRegistration(t *testing.T) {
	encCfg := moduletestutil.MakeTestEncodingConfig()
	appModule := module.AppModuleBasic{}

	t.Run("legacy_amino_codec", func(t *testing.T) {
		// Test that legacy amino codec registration doesn't panic
		require.NotPanics(t, func() {
			legacyAmino := encCfg.Amino
			appModule.RegisterLegacyAminoCodec(legacyAmino)
		})
	})

	t.Run("interface_registry", func(t *testing.T) {
		// Test that interface registration doesn't panic
		require.NotPanics(t, func() {
			appModule.RegisterInterfaces(encCfg.InterfaceRegistry)
		})
	})
}

// Test module constants and metadata
func TestModuleConstants(t *testing.T) {
	t.Run("consensus_version", func(t *testing.T) {
		require.Equal(t, uint64(1), module.ConsensusVersion)
	})

	t.Run("module_name", func(t *testing.T) {
		require.Equal(t, "crosschain", types.ModuleName)
	})

	t.Run("store_key", func(t *testing.T) {
		require.Equal(t, types.ModuleName, types.StoreKey)
	})
}

// Benchmark module operations
func BenchmarkModuleGenesis(b *testing.B) {
	encCfg := moduletestutil.MakeTestEncodingConfig()
	appModule := module.AppModuleBasic{}

	genesisState := types.GenesisState{
		Params: types.Params{
			Admin: "push1234567890123456789012345678901234567890",
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		genesis, _ := encCfg.Codec.MarshalJSON(&genesisState)
		_ = appModule.ValidateGenesis(encCfg.Codec, nil, genesis)
	}
}
