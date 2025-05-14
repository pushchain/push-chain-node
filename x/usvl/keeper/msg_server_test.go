package keeper_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/push-protocol/push-chain/x/usvl/keeper"
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

func TestVerifyExternalTransaction(t *testing.T) {
	require := require.New(t)

	// Define test cases with real transaction data from Ethereum Sepolia
	testCases := []struct {
		name     string
		request  *types.MsgVerifyExternalTransaction
		err      bool
		verified bool
	}{
		{
			name: "valid transaction verification",
			request: &types.MsgVerifyExternalTransaction{
				Sender:      "sender", // Will be replaced with a real address
				TxHash:      "0xcea123f57055dcf5d673008b094d27f5207e696674e05637cd6ba1ef0abc2e36",
				CaipAddress: "eip155:11155111:0xFcEAf1850965F7E601d4E468DB14321fC1Ba17eB",
			},
			err:      false,
			verified: true,
		},
		{
			name: "valid transaction but wrong address",
			request: &types.MsgVerifyExternalTransaction{
				Sender:      "sender", // Will be replaced with a real address
				TxHash:      "0xcea123f57055dcf5d673008b094d27f5207e696674e05637cd6ba1ef0abc2e36",
				CaipAddress: "eip155:11155111:0x3Dfc53E3C77bb4e30Ce333Be1a66Ce62558bE395",
			},
			err:      false,
			verified: false,
		},
		{
			name: "invalid transaction hash",
			request: &types.MsgVerifyExternalTransaction{
				Sender:      "sender", // Will be replaced with a real address
				TxHash:      "0xinvalidtxhash",
				CaipAddress: "eip155:11155111:0xFcEAf1850965F7E601d4E468DB14321fC1Ba17eB",
			},
			err:      true,
			verified: false,
		},
		{
			name: "invalid CAIP address format",
			request: &types.MsgVerifyExternalTransaction{
				Sender:      "sender", // Will be replaced with a real address
				TxHash:      "0xcea123f57055dcf5d673008b094d27f5207e696674e05637cd6ba1ef0abc2e36",
				CaipAddress: "invalid:address:format",
			},
			err:      true,
			verified: false,
		},
		{
			name: "unsupported chain",
			request: &types.MsgVerifyExternalTransaction{
				Sender:      "sender", // Will be replaced with a real address
				TxHash:      "0xcea123f57055dcf5d673008b094d27f5207e696674e05637cd6ba1ef0abc2e36",
				CaipAddress: "eip155:1:0xFcEAf1850965F7E601d4E468DB14321fC1Ba17eB", // Ethereum mainnet not in our config
			},
			err:      true,
			verified: false,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			// Create a fresh test fixture for each test case
			f := SetupTest(t)

			// Update sender with actual address from fixture
			tc.request.Sender = f.addrs[0].String()

			// Skip long-running external RPC calls in CI or quick test runs
			if testing.Short() && tc.name != "invalid CAIP address format" && tc.name != "unsupported chain" {
				t.Skip("Skipping RPC test in short mode")
			}

			// Add a chain configuration for Ethereum Sepolia testnet
			sepoliaConfig := &types.MsgAddChainConfig{
				Authority: f.govModAddr,
				ChainConfig: types.ChainConfig{
					ChainId:               "11155111",
					ChainName:             "Ethereum Sepolia",
					CaipPrefix:            "eip155:11155111",
					LockerContractAddress: "0x57235d27c8247CFE0E39248c9c9F22BD6EB054e1",
					UsdcAddress:           "0x7169D38820dfd117C3FA1f22a697dBA58d90BA06",
					PublicRpcUrl:          "https://ethereum-sepolia.publicnode.com",
					NetworkType:           types.NetworkTypeTestnet,
					VmType:                types.VmTypeEvm,
				},
			}
			_, err := f.msgServer.AddChainConfig(f.ctx, sepoliaConfig)
			require.NoError(err)

			resp, err := f.msgServer.VerifyExternalTransaction(f.ctx, tc.request)

			if tc.err {
				require.Error(err)
			} else {
				require.NoError(err)
				require.NotNil(resp)
				require.Equal(tc.verified, resp.Verified)

				// If verified, check that we have transaction info
				if tc.verified {
					require.NotEmpty(resp.TxInfo)
					t.Logf("Transaction info: %s", resp.TxInfo)
				}
			}

			// Check that events were emitted properly for both successful and failed verifications
			if !tc.err {
				events := f.ctx.EventManager().Events()

				// Should find the event for the verification (whether successful or not)
				found := false
				for _, event := range events {
					if event.Type == types.EventTypeExternalTransactionVerified {
						found = true
						// Check that the event has the expected attributes
						for _, attr := range event.Attributes {
							if string(attr.Key) == "tx_hash" {
								require.Equal(tc.request.TxHash, string(attr.Value))
							}
							if string(attr.Key) == "caip_address" {
								require.Equal(tc.request.CaipAddress, string(attr.Value))
							}
							if string(attr.Key) == "verified" {
								require.Equal(fmt.Sprintf("%t", tc.verified), string(attr.Value))
							}
						}
					}
				}
				require.True(found, "Expected external transaction verification event not found")
			}
		})
	}
}

func TestVerifyExternalTransactionDuplicates(t *testing.T) {
	// Skip the test in short mode to avoid making external API calls in CI
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	f := SetupTest(t)
	require := require.New(t)

	// Add a chain configuration for Ethereum Sepolia testnet with block confirmation set to 1
	// to make the test simpler
	sepoliaConfig := &types.MsgAddChainConfig{
		Authority: f.govModAddr,
		ChainConfig: types.ChainConfig{
			ChainId:               "11155111",
			ChainName:             "Ethereum Sepolia",
			CaipPrefix:            "eip155:11155111",
			LockerContractAddress: "0x57235d27c8247CFE0E39248c9c9F22BD6EB054e1",
			UsdcAddress:           "0x7169D38820dfd117C3FA1f22a697dBA58d90BA06",
			PublicRpcUrl:          "https://ethereum-sepolia.publicnode.com",
			NetworkType:           types.NetworkTypeTestnet,
			VmType:                types.VmTypeEvm,
			BlockConfirmation:     1, // Just require 1 confirmation for testing
		},
	}
	_, err := f.msgServer.AddChainConfig(f.ctx, sepoliaConfig)
	require.NoError(err)

	// Using real transactions from Sepolia testnet
	// Make sure this is a valid transaction that exists on Sepolia
	txHash := "0xcea123f57055dcf5d673008b094d27f5207e696674e05637cd6ba1ef0abc2e36"

	// This should be the real sender address of the transaction above
	senderAddress := "0xFcEAf1850965F7E601d4E468DB14321fC1Ba17eB"
	caipAddress := "eip155:11155111:" + senderAddress

	// Clean up the KV store after test
	defer func() {
		// Clean up the KV store by removing all verified transactions
		// This ensures tests don't interfere with each other
		iterator, err := f.k.VerifiedTxs.Iterate(f.ctx, nil)
		require.NoError(err)
		defer iterator.Close()

		for ; iterator.Valid(); iterator.Next() {
			key, err := iterator.Key()
			require.NoError(err, "Failed to get key from iterator")
			err = f.k.VerifiedTxs.Remove(f.ctx, key)
			require.NoError(err, "Failed to clean up KV store")
		}
	}()

	// 1. First verification should succeed
	resp, err := f.msgServer.VerifyExternalTransaction(f.ctx, &types.MsgVerifyExternalTransaction{
		Sender:      f.addrs[0].String(),
		TxHash:      txHash,
		CaipAddress: caipAddress,
	})

	if err != nil {
		t.Logf("Verification error: %v", err)
		t.Skip("Skipping rest of test due to verification error - the transaction may not exist or node could be unavailable")
		return
	}

	require.NoError(err)
	require.NotNil(resp)
	require.True(resp.Verified)
	require.NotEmpty(resp.TxInfo)
	t.Logf("First verification successful: %s", resp.TxInfo)

	// 2. Second verification of the same transaction should fail
	_, err = f.msgServer.VerifyExternalTransaction(f.ctx, &types.MsgVerifyExternalTransaction{
		Sender:      f.addrs[0].String(),
		TxHash:      txHash,
		CaipAddress: caipAddress,
	})
	require.Error(err)
	require.Contains(err.Error(), "has already been verified")
	t.Logf("Second verification correctly failed with: %v", err)

	// 3. Verify that the transaction was stored in the KV store
	key := keeper.CreateTxKey(txHash, caipAddress)
	hasKey, err := f.k.VerifiedTxs.Has(f.ctx, key)
	require.NoError(err)
	require.True(hasKey, "Transaction was not stored in KV store")

	// 4. Retrieve the stored transaction and verify its data
	serialized, err := f.k.VerifiedTxs.Get(f.ctx, key)
	require.NoError(err)

	var storedTx types.VerifiedTransaction
	err = json.Unmarshal([]byte(serialized), &storedTx)
	require.NoError(err)

	require.Equal(txHash, storedTx.TxHash)
	require.Equal(caipAddress, storedTx.CaipAddress)
	require.Equal("11155111", storedTx.ChainId)
	require.False(storedTx.VerifiedAt.IsZero(), "Verification timestamp should be set")

	t.Logf("Stored transaction: %+v", storedTx)

	// Try verifying with a different address (should fail since it doesn't match the actual sender)
	differentAddress := "eip155:11155111:0x1234567890123456789012345678901234567890"
	_, err = f.msgServer.VerifyExternalTransaction(f.ctx, &types.MsgVerifyExternalTransaction{
		Sender:      f.addrs[0].String(),
		TxHash:      txHash,
		CaipAddress: differentAddress,
	})
	// This should fail because the addresses don't match
	require.Error(err)
	require.Contains(err.Error(), "Transaction exists but is from")

	// Verify our cleanup works by removing a key manually and checking it's gone
	err = f.k.VerifiedTxs.Remove(f.ctx, key)
	require.NoError(err, "Failed to remove key from KV store")

	hasKey, err = f.k.VerifiedTxs.Has(f.ctx, key)
	require.NoError(err)
	require.False(hasKey, "Key was not properly removed from KV store")
}
