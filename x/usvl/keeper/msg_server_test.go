package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/push-protocol/push-chain/x/usvl/types"
)

func TestParams(t *testing.T) {
	f := SetupTest(t)
	require := require.New(t)

	testCases := []struct {
		name    string
		request *types.MsgUpdateParams
		err     bool
	}{
		{
			name: "fail; invalid authority",
			request: &types.MsgUpdateParams{
				Authority: f.addrs[0].String(),
				Params:    types.DefaultParams(),
			},
			err: true,
		},
		{
			name: "success",
			request: &types.MsgUpdateParams{
				Authority: f.govModAddr,
				Params:    types.DefaultParams(),
			},
			err: false,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			_, err := f.msgServer.UpdateParams(f.ctx, tc.request)

			if tc.err {
				require.Error(err)
			} else {
				require.NoError(err)

				r, err := f.queryServer.Params(f.ctx, &types.QueryParamsRequest{})
				require.NoError(err)

				require.EqualValues(&tc.request.Params, r.Params)
			}

		})
	}
}

func TestAddChainConfig(t *testing.T) {
	f := SetupTest(t)
	require := require.New(t)

	// Define valid chain config params
	validChainId := "1"
	validChainName := "Ethereum Mainnet"
	validCaipPrefix := "eip155:1"
	validLockerContract := "0x1234567890AbCdEf1234567890AbCdEf12345678"
	validUsdcAddress := "0xabcdef1234567890AbCdEf1234567890AbCdEf12"
	validRpcUrl := "https://ethereum-rpc.example.com"

	testCases := []struct {
		name    string
		request *types.MsgAddChainConfig
		err     bool
	}{
		{
			name: "fail; invalid authority",
			request: &types.MsgAddChainConfig{
				Authority: f.addrs[0].String(),
				ChainConfig: types.ChainConfig{
					ChainId:               validChainId,
					ChainName:             validChainName,
					CaipPrefix:            validCaipPrefix,
					LockerContractAddress: validLockerContract,
					UsdcAddress:           validUsdcAddress,
					PublicRpcUrl:          validRpcUrl,
				},
			},
			err: true,
		},
		{
			name: "success",
			request: &types.MsgAddChainConfig{
				Authority: f.govModAddr,
				ChainConfig: types.ChainConfig{
					ChainId:               validChainId,
					ChainName:             validChainName,
					CaipPrefix:            validCaipPrefix,
					LockerContractAddress: validLockerContract,
					UsdcAddress:           validUsdcAddress,
					PublicRpcUrl:          validRpcUrl,
				},
			},
			err: false,
		},
		{
			name: "fail; invalid chain ID",
			request: &types.MsgAddChainConfig{
				Authority: f.govModAddr,
				ChainConfig: types.ChainConfig{
					ChainId:               "", // Empty chain ID
					ChainName:             validChainName,
					CaipPrefix:            validCaipPrefix,
					LockerContractAddress: validLockerContract,
					UsdcAddress:           validUsdcAddress,
					PublicRpcUrl:          validRpcUrl,
				},
			},
			err: true,
		},
		{
			name: "fail; invalid CAIP prefix",
			request: &types.MsgAddChainConfig{
				Authority: f.govModAddr,
				ChainConfig: types.ChainConfig{
					ChainId:               validChainId,
					ChainName:             validChainName,
					CaipPrefix:            "invalid", // Invalid CAIP prefix
					LockerContractAddress: validLockerContract,
					UsdcAddress:           validUsdcAddress,
					PublicRpcUrl:          validRpcUrl,
				},
			},
			err: true,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			_, err := f.msgServer.AddChainConfig(f.ctx, tc.request)

			if tc.err {
				require.Error(err)
			} else {
				require.NoError(err)

				// Query to verify chain config was added
				r, err := f.queryServer.ChainConfig(f.ctx, &types.QueryChainConfigRequest{
					ChainId: tc.request.ChainConfig.ChainId,
				})
				require.NoError(err)
				require.NotNil(r.ChainConfig)
				require.Equal(tc.request.ChainConfig.ChainId, r.ChainConfig.ChainId)
				require.Equal(tc.request.ChainConfig.ChainName, r.ChainConfig.ChainName)
				require.Equal(tc.request.ChainConfig.CaipPrefix, r.ChainConfig.CaipPrefix)
				require.Equal(tc.request.ChainConfig.LockerContractAddress, r.ChainConfig.LockerContractAddress)
				require.Equal(tc.request.ChainConfig.UsdcAddress, r.ChainConfig.UsdcAddress)
				require.Equal(tc.request.ChainConfig.PublicRpcUrl, r.ChainConfig.PublicRpcUrl)
			}
		})
	}
}

func TestUpdateChainConfig(t *testing.T) {
	f := SetupTest(t)
	require := require.New(t)

	// Define valid chain config params
	validChainId := "1"
	validChainName := "Ethereum Mainnet"
	validCaipPrefix := "eip155:1"
	validLockerContract := "0x1234567890AbCdEf1234567890AbCdEf12345678"
	validUsdcAddress := "0xabcdef1234567890AbCdEf1234567890AbCdEf12"
	validRpcUrl := "https://ethereum-rpc.example.com"

	// First add a chain config that we can update
	addMsg := &types.MsgAddChainConfig{
		Authority: f.govModAddr,
		ChainConfig: types.ChainConfig{
			ChainId:               validChainId,
			ChainName:             validChainName,
			CaipPrefix:            validCaipPrefix,
			LockerContractAddress: validLockerContract,
			UsdcAddress:           validUsdcAddress,
			PublicRpcUrl:          validRpcUrl,
		},
	}
	_, err := f.msgServer.AddChainConfig(f.ctx, addMsg)
	require.NoError(err)

	// Now test update cases
	testCases := []struct {
		name    string
		request *types.MsgUpdateChainConfig
		err     bool
	}{
		{
			name: "fail; invalid authority",
			request: &types.MsgUpdateChainConfig{
				Authority: f.addrs[0].String(),
				ChainConfig: types.ChainConfig{
					ChainId:               validChainId,
					ChainName:             "Updated Name",
					CaipPrefix:            validCaipPrefix,
					LockerContractAddress: "0x9876543210AbCdEf9876543210AbCdEf98765432",
					UsdcAddress:           "0xfedcba9876543210FeDcBa9876543210FeDcBa98",
					PublicRpcUrl:          "https://updated-ethereum-rpc.example.com",
				},
			},
			err: true,
		},
		{
			name: "success",
			request: &types.MsgUpdateChainConfig{
				Authority: f.govModAddr,
				ChainConfig: types.ChainConfig{
					ChainId:               validChainId,
					ChainName:             "Updated Name",
					CaipPrefix:            validCaipPrefix,
					LockerContractAddress: "0x9876543210AbCdEf9876543210AbCdEf98765432",
					UsdcAddress:           "0xfedcba9876543210FeDcBa9876543210FeDcBa98",
					PublicRpcUrl:          "https://updated-ethereum-rpc.example.com",
				},
			},
			err: false,
		},
		{
			name: "fail; non-existent chain ID",
			request: &types.MsgUpdateChainConfig{
				Authority: f.govModAddr,
				ChainConfig: types.ChainConfig{
					ChainId:               "non-existent-chain",
					ChainName:             "Updated Name",
					CaipPrefix:            validCaipPrefix,
					LockerContractAddress: validLockerContract,
					UsdcAddress:           validUsdcAddress,
					PublicRpcUrl:          validRpcUrl,
				},
			},
			err: true,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			_, err := f.msgServer.UpdateChainConfig(f.ctx, tc.request)

			if tc.err {
				require.Error(err)
			} else {
				require.NoError(err)

				// Query to verify chain config was updated
				r, err := f.queryServer.ChainConfig(f.ctx, &types.QueryChainConfigRequest{
					ChainId: tc.request.ChainConfig.ChainId,
				})
				require.NoError(err)
				require.NotNil(r.ChainConfig)
				require.Equal(tc.request.ChainConfig.ChainId, r.ChainConfig.ChainId)
				require.Equal(tc.request.ChainConfig.ChainName, r.ChainConfig.ChainName)
				require.Equal(tc.request.ChainConfig.CaipPrefix, r.ChainConfig.CaipPrefix)
				require.Equal(tc.request.ChainConfig.LockerContractAddress, r.ChainConfig.LockerContractAddress)
				require.Equal(tc.request.ChainConfig.UsdcAddress, r.ChainConfig.UsdcAddress)
				require.Equal(tc.request.ChainConfig.PublicRpcUrl, r.ChainConfig.PublicRpcUrl)
			}
		})
	}
}

func TestDeleteChainConfig(t *testing.T) {
	f := SetupTest(t)
	require := require.New(t)

	// Define valid chain config params
	validChainId := "1"
	validChainName := "Ethereum Mainnet"
	validCaipPrefix := "eip155:1"
	validLockerContract := "0x1234567890AbCdEf1234567890AbCdEf12345678"
	validUsdcAddress := "0xabcdef1234567890AbCdEf1234567890AbCdEf12"
	validRpcUrl := "https://ethereum-rpc.example.com"

	// First add a chain config that we can delete
	addMsg := &types.MsgAddChainConfig{
		Authority: f.govModAddr,
		ChainConfig: types.ChainConfig{
			ChainId:               validChainId,
			ChainName:             validChainName,
			CaipPrefix:            validCaipPrefix,
			LockerContractAddress: validLockerContract,
			UsdcAddress:           validUsdcAddress,
			PublicRpcUrl:          validRpcUrl,
		},
	}
	_, err := f.msgServer.AddChainConfig(f.ctx, addMsg)
	require.NoError(err)

	// Now test delete cases
	testCases := []struct {
		name    string
		request *types.MsgDeleteChainConfig
		err     bool
	}{
		{
			name: "fail; invalid authority",
			request: &types.MsgDeleteChainConfig{
				Authority: f.addrs[0].String(),
				ChainId:   validChainId,
			},
			err: true,
		},
		{
			name: "fail; non-existent chain ID",
			request: &types.MsgDeleteChainConfig{
				Authority: f.govModAddr,
				ChainId:   "non-existent-chain",
			},
			err: true,
		},
		{
			name: "success",
			request: &types.MsgDeleteChainConfig{
				Authority: f.govModAddr,
				ChainId:   validChainId,
			},
			err: false,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			_, err := f.msgServer.DeleteChainConfig(f.ctx, tc.request)

			if tc.err {
				require.Error(err)
			} else {
				require.NoError(err)

				// Query to verify chain config was deleted
				_, err := f.queryServer.ChainConfig(f.ctx, &types.QueryChainConfigRequest{
					ChainId: tc.request.ChainId,
				})
				require.Error(err, "Chain config should be deleted")
			}
		})
	}
}

func TestQueryChainConfigs(t *testing.T) {
	f := SetupTest(t)
	require := require.New(t)

	// Define multiple chain configs
	chainConfigs := []types.ChainConfig{
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

	// Add all chain configs
	for _, config := range chainConfigs {
		_, err := f.msgServer.AddChainConfig(f.ctx, &types.MsgAddChainConfig{
			Authority:   f.govModAddr,
			ChainConfig: config,
		})
		require.NoError(err)
	}

	// Test querying all chain configs
	r, err := f.queryServer.ChainConfigs(f.ctx, &types.QueryChainConfigsRequest{})
	require.NoError(err)
	require.NotNil(r)
	require.Len(r.ChainConfigs, len(chainConfigs))

	// Check that all expected chain IDs are present
	chainIdMap := make(map[string]bool)
	for _, config := range chainConfigs {
		chainIdMap[config.ChainId] = true
	}

	for _, config := range r.ChainConfigs {
		require.True(chainIdMap[config.ChainId])
	}

	// Test querying specific chain config
	specificR, err := f.queryServer.ChainConfig(f.ctx, &types.QueryChainConfigRequest{
		ChainId: "137",
	})
	require.NoError(err)
	require.NotNil(specificR)
	require.Equal("137", specificR.ChainConfig.ChainId)
	require.Equal("Polygon Mainnet", specificR.ChainConfig.ChainName)
}
