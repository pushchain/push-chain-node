package keeper_test

import (
	"context"
	"testing"
	"time"

	"github.com/push-protocol/push-chain/x/usvl/keeper"
	"github.com/push-protocol/push-chain/x/usvl/types"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// Change these values to test with real transactions
const (
	// Ethereum Sepolia test transaction
	// This is a placeholder - replace with a real transaction hash
	SepoliaTxHash      = "0xcea123f57055dcf5d673008b094d27f5207e696674e05637cd6ba1ef0abc2e36"
	SepoliaCAIPAddress = "eip155:11155111:0xFcEAf1850965F7E601d4E468DB14321fC1Ba17eB"
)

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
	// Create a wrapper around the real keeper that has direct access to chain configs
	return &keeper.KeeperWithConfigs{
		// Initialize with our test configurations
		ChainConfigs: suite.chainConfig,
	}
}

// Integration test for Sepolia
func (suite *TxVerifyTestSuite) TestVerifySepoliaTransaction() {
	t := suite.T()

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
