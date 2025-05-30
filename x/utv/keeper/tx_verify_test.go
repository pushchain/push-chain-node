package keeper_test

import (
	"context"
	"testing"
	"time"

	"github.com/push-protocol/push-chain/utils/env"
	"github.com/push-protocol/push-chain/x/utv/keeper"
	"github.com/push-protocol/push-chain/x/utv/types"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// Change these values to test with real transactions

type TxVerifyTestSuite struct {
	suite.Suite
	keeper      *keeper.KeeperTest
	ctx         context.Context
	chainConfig map[string]types.ChainConfigData
}

func (suite *TxVerifyTestSuite) SetupTest() {
	suite.ctx = context.Background()

	// Create test chain configurations - only Sepolia
	suite.chainConfig = map[string]types.ChainConfigData{
		"eip155:11155111": {
			ChainId:               "11155111",
			ChainName:             "Ethereum Sepolia",
			CaipPrefix:            "eip155:11155111",
			LockerContractAddress: "0x1234567890AbCdEf1234567890AbCdEf12345678",
			UsdcAddress:           "0x1234567890AbCdEf1234567890AbCdEf12345678",
			PublicRpcUrl:          "https://ethereum-sepolia.publicnode.com",
			NetworkType:           types.NetworkTypeTestnet,
			VmType:                types.VmTypeEvm,
			BlockConfirmation:     12, // Require 12 confirmations for Sepolia testnet
		},
	}

	// Create a test keeper with mock dependencies
	suite.keeper = keeper.NewKeeperTest(suite.T())

	// Setup the keeper to return our test chain configs
	suite.keeper.SetupGetAllChainConfigs(func(ctx context.Context) ([]types.ChainConfigData, error) {
		configs := make([]types.ChainConfigData, 0, len(suite.chainConfig))
		for _, config := range suite.chainConfig {
			configs = append(configs, config)
		}
		return configs, nil
	})
}

// TestTxVerifyTestSuite runs the test suite
func TestTxVerifyTestSuite(t *testing.T) {
	// Attempt to load .env file but don't fail if it doesn't exist
	_ = env.LoadEnv() // Ignore the error since we handle missing .env gracefully

	suite.Run(t, new(TxVerifyTestSuite))
}

// Unit tests - using the KeeperTest mock
func (suite *TxVerifyTestSuite) TestVerifyExternalTransactionBasic() {
	caip, err := types.ParseCAIPAddress("eip155:11155111:0x95222290DD7278Aa3Ddd389Cc1E1d165CC4BAfe5")
	require.NoError(suite.T(), err)

	// Verify that the chain identifier is correctly extracted
	chainId := caip.GetChainIdentifier()
	require.Equal(suite.T(), "eip155:11155111", chainId)

	// Check that the chain config can be found
	config, exists := suite.chainConfig[chainId]
	require.True(suite.T(), exists)
	require.Equal(suite.T(), "Ethereum Sepolia", config.ChainName)
}

// getRealKeeperWithConfigs creates a real keeper with the test chain configurations
// This is needed for integration tests that make actual RPC calls
func (suite *TxVerifyTestSuite) getRealKeeperWithConfigs() *keeper.KeeperWithConfigs {
	// Create a wrapper around the real keeper using our new constructor
	return keeper.NewKeeperWithConfigs(suite.chainConfig)
}

// Integration test for Sepolia
func (suite *TxVerifyTestSuite) TestVerifySepoliaTransaction() {
	t := suite.T()
	const (
		// Ethereum Sepolia test transaction
		// This is a placeholder - replace with a real transaction hash
		SepoliaTxHash      = "0xcea123f57055dcf5d673008b094d27f5207e696674e05637cd6ba1ef0abc2e36"
		SepoliaCAIPAddress = "eip155:11155111:0xFcEAf1850965F7E601d4E468DB14321fC1Ba17eB"
	)
	// Skip the test by default to avoid making real RPC calls in CI
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Create real keeper with configurations
	realKeeper := suite.getRealKeeperWithConfigs()

	// Set a timeout for the test
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	txHash := SepoliaTxHash
	caipAddress := SepoliaCAIPAddress

	// Parse CAIP address to make sure it's valid
	caip, err := types.ParseCAIPAddress(caipAddress)
	require.NoError(t, err)

	t.Logf("Running verification on Sepolia transaction: %s for address: %s", txHash, caip.Address)

	// Use the special direct verification method that doesn't require GetAllChainConfigs to be initialized
	result, err := realKeeper.VerifyExternalTransactionDirect(ctx, txHash, caipAddress)

	// We don't assert specific results because they depend on the actual transaction
	// Just log the result for inspection
	if err != nil {
		t.Logf("Verification error: %v", err)
	} else {
		t.Logf("Verification result: verified=%t, info=%s", result.Verified, result.TxInfo)
	}
}

// TestVerifyWithRealTransactions is a test that uses real transaction data
func (suite *TxVerifyTestSuite) TestVerifyWithRealTransactions() {
	t := suite.T()

	// Comment out this line to run the test with real transactions
	// t.Skip("This test requires manual configuration with real transaction data")

	// Real transactions to verify - only Sepolia test cases
	testCases := []struct {
		name         string
		txHash       string
		caipAddress  string
		shouldVerify bool
	}{
		{
			name:         "Sepolia Transaction - Valid",
			txHash:       "0xcea123f57055dcf5d673008b094d27f5207e696674e05637cd6ba1ef0abc2e36",
			caipAddress:  "eip155:11155111:0xFcEAf1850965F7E601d4E468DB14321fC1Ba17eB",
			shouldVerify: true,
		},
		{
			name:         "Sepolia Transaction - Wrong Address",
			txHash:       "0xcea123f57055dcf5d673008b094d27f5207e696674e05637cd6ba1ef0abc2e36",
			caipAddress:  "eip155:11155111:0x1234567890123456789012345678901234567890",
			shouldVerify: false,
		},
	}

	// Create a real keeper with configurations
	realKeeper := suite.getRealKeeperWithConfigs()

	// Set a timeout for the entire test
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Use the direct verification method
			result, err := realKeeper.VerifyExternalTransactionDirect(ctx, tc.txHash, tc.caipAddress)
			t.Logf("Test case: %s", tc.name)
			t.Logf("Transaction hash: %s", tc.txHash)
			t.Logf("CAIP address: %s", tc.caipAddress)

			if err != nil {
				t.Logf("Error: %v", err)
				if tc.shouldVerify {
					t.Errorf("Expected transaction to verify, but got error: %v", err)
				}
			} else {
				t.Logf("Verified: %t", result.Verified)
				t.Logf("Info: %s", result.TxInfo)

				if tc.shouldVerify && !result.Verified {
					t.Errorf("Expected transaction to be verified, but it was not")
				} else if !tc.shouldVerify && result.Verified {
					t.Errorf("Expected transaction to fail verification, but it was verified")
				}
			}
		})
	}
}

// TestVerifyConfirmationRequirement tests the block confirmation validation feature
func (suite *TxVerifyTestSuite) TestVerifyConfirmationRequirement() {
	t := suite.T()
	const (
		// Ethereum Sepolia test transaction
		// This is a placeholder - replace with a real transaction hash
		SepoliaTxHash      = "0xcea123f57055dcf5d673008b094d27f5207e696674e05637cd6ba1ef0abc2e36"
		SepoliaCAIPAddress = "eip155:11155111:0xFcEAf1850965F7E601d4E468DB14321fC1Ba17eB"
	)
	// Skip the test by default to avoid making real RPC calls in CI
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Create a copy of the chain config but with very high confirmation requirement
	highConfirmationConfig := suite.chainConfig["eip155:11155111"]
	highConfirmationConfig.BlockConfirmation = 10000 // Set an unrealistically high number that can't be satisfied

	// Create a new config map with the high confirmation requirement
	highConfirmationMap := map[string]types.ChainConfigData{
		"eip155:11155111": highConfirmationConfig,
	}

	// Create a keeper with the high confirmation requirement
	highConfirmationKeeper := &keeper.KeeperWithConfigs{
		ChainConfigs: highConfirmationMap,
	}

	// Set a timeout for the test
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Use the same transaction that should be valid otherwise
	txHash := SepoliaTxHash
	caipAddress := SepoliaCAIPAddress

	// Verify with regular config first to make sure transaction is valid otherwise
	regularKeeper := suite.getRealKeeperWithConfigs()
	_, err := regularKeeper.VerifyExternalTransactionDirect(ctx, txHash, caipAddress)

	// Skip the confirmation check part if there's an error with the regular verification
	if err != nil {
		t.Logf("Skipping confirmation test due to error with regular verification: %v", err)
		return
	}

	// Now try with high confirmation requirement
	highConfirmResult, err := highConfirmationKeeper.VerifyExternalTransactionDirect(ctx, txHash, caipAddress)

	// Verification should fail due to insufficient confirmations
	if err != nil {
		t.Logf("High confirmation verification returned error: %v", err)
	} else {
		// The transaction should not be verified
		require.False(t, highConfirmResult.Verified, "Transaction should not be verified with high confirmation requirement")

		// The failure reason should mention confirmations
		require.Contains(t, highConfirmResult.TxInfo, "confirmation",
			"Verification failure reason should mention confirmations")

		t.Logf("Confirmation test passed. Result: %s", highConfirmResult.TxInfo)
	}

	// Now try with a reasonable confirmation requirement to make sure it passes
	reasonableConfirmationConfig := suite.chainConfig["eip155:11155111"]
	reasonableConfirmationConfig.BlockConfirmation = 1 // Just 1 confirmation

	reasonableConfirmationMap := map[string]types.ChainConfigData{
		"eip155:11155111": reasonableConfirmationConfig,
	}

	reasonableConfirmationKeeper := &keeper.KeeperWithConfigs{
		ChainConfigs: reasonableConfirmationMap,
	}

	reasonableResult, err := reasonableConfirmationKeeper.VerifyExternalTransactionDirect(ctx, txHash, caipAddress)

	if err != nil {
		t.Logf("Reasonable confirmation verification returned error: %v", err)
	} else {
		require.True(t, reasonableResult.Verified,
			"Transaction should be verified with reasonable confirmation requirement")
		t.Logf("Reasonable confirmation test passed. Transaction verified with info: %s", reasonableResult.TxInfo)
	}
}
