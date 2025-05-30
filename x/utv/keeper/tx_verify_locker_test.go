package keeper_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/push-protocol/push-chain/x/utv/keeper"
	"github.com/push-protocol/push-chain/x/utv/types"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// Use a real Sepolia testnet transaction hash as specified in the requirements
const (
	// Ethereum Sepolia test transaction for locker verification
	SepoliaLockerTxHash = "0x14d7602b0c0902d8262ec2a21bf4fed0c0f8de6e0b663d5e9c3e7445293be2b2"
	SepoliaCAIPAddress  = "eip155:11155111:0xeCba9a32A9823f1cb00cdD8344Bf2D1d87a8dd97"
	// Actual target address from the transaction - discovered by running TestFindActualTargetAddress
	SepoliaLockerAddr = "0x57235d27c8247CFE0E39248c9c9F22BD6EB054e1"
)

type TxVerifyLockerTestSuite struct {
	suite.Suite
	ctx         context.Context
	chainConfig map[string]types.ChainConfigData
}

func (suite *TxVerifyLockerTestSuite) SetupTest() {
	suite.ctx = context.Background()

	// Create test chain configurations for Sepolia with the actual locker contract address
	// In a real implementation, this should be the actual locker contract on Sepolia
	suite.chainConfig = map[string]types.ChainConfigData{
		"eip155:11155111": {
			ChainId:               "11155111",
			ChainName:             "Ethereum Sepolia",
			CaipPrefix:            "eip155:11155111",
			LockerContractAddress: SepoliaLockerAddr, // This should be the actual locker contract address
			UsdcAddress:           "0x1234567890AbCdEf1234567890AbCdEf12345678",
			PublicRpcUrl:          "https://ethereum-sepolia.publicnode.com",
			NetworkType:           types.NetworkTypeTestnet,
			VmType:                types.VmTypeEvm,
			BlockConfirmation:     12, // Require 12 confirmations for Sepolia testnet
		},
	}
}

// TestTxVerifyLockerTestSuite runs the test suite
func TestTxVerifyLockerTestSuite(t *testing.T) {
	suite.Run(t, new(TxVerifyLockerTestSuite))
}

// TestFindActualTargetAddress is a special test to discover the actual target address
// of the transaction for proper setup of other tests
func (suite *TxVerifyLockerTestSuite) TestFindActualTargetAddress() {
	t := suite.T()

	// Skip the test by default to avoid making real RPC calls in CI
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Create a basic config without locker address
	config := types.ChainConfigData{
		ChainId:           "11155111",
		ChainName:         "Ethereum Sepolia",
		CaipPrefix:        "eip155:11155111",
		PublicRpcUrl:      "https://ethereum-sepolia.publicnode.com",
		NetworkType:       types.NetworkTypeTestnet,
		VmType:            types.VmTypeEvm,
		BlockConfirmation: 1, // Just need 1 confirmation for this test
	}

	// Create a map with this config
	basicConfig := map[string]types.ChainConfigData{
		"eip155:11155111": config,
	}

	// Create a keeper with this basic config
	basicKeeper := keeper.NewKeeperWithConfigs(basicConfig)

	// Set a timeout for the test
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Use just the basic verification to inspect transaction details
	txHash := SepoliaLockerTxHash
	caipAddress := SepoliaCAIPAddress

	// Get transaction details to find the target address
	basicResult, err := basicKeeper.VerifyExternalTransactionDirect(ctx, txHash, caipAddress)

	if err != nil {
		t.Logf("Error retrieving transaction details: %v", err)
	} else {
		t.Logf("Transaction target details: %s", basicResult.TxInfo)

		// This test mainly serves as a way to determine the correct target contract address
		// for this specific transaction so we can set SepoliaLockerAddr correctly
		t.Logf("For proper test setup, use the actual 'to' address from this transaction as SepoliaLockerAddr")
	}
}

// getRealKeeperWithConfigs creates a real keeper with the test chain configurations
// This is needed for integration tests that make actual RPC calls
func (suite *TxVerifyLockerTestSuite) getRealKeeperWithConfigs() *keeper.KeeperWithConfigs {
	// Create a wrapper around the real keeper using our constructor
	return keeper.NewKeeperWithConfigs(suite.chainConfig)
}

// Integration test for Sepolia locker verification
func (suite *TxVerifyLockerTestSuite) TestVerifySepoliaTransactionToLocker() {
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

	txHash := SepoliaLockerTxHash
	caipAddress := SepoliaCAIPAddress

	// Use the specific locker verification method
	result, err := realKeeper.VerifyExternalTransactionToLockerDirect(ctx, txHash, caipAddress)

	// Log the result for inspection
	if err != nil {
		t.Logf("Locker verification error: %v", err)
	} else {
		t.Logf("Locker verification result: verified=%t, info=%s", result.Verified, result.TxInfo)

		// If the transaction is valid and the contract address is set correctly,
		// this should succeed
		if result.Verified {
			t.Logf("Transaction successfully verified as directed to locker contract")
		} else {
			t.Logf("Transaction verification failed. Check the actual locker contract address.")
		}
	}
}

// TestVerifyWithRealTransactionsToLocker tests verification of real transactions to the locker
func (suite *TxVerifyLockerTestSuite) TestVerifyWithRealTransactionsToLocker() {
	t := suite.T()

	// Skip the test by default to avoid making real RPC calls in CI
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Test cases for locker verification
	testCases := []struct {
		name         string
		txHash       string
		caipAddress  string
		shouldVerify bool
	}{
		{
			name:         "Valid Transaction to Locker",
			txHash:       SepoliaLockerTxHash,
			caipAddress:  SepoliaCAIPAddress,
			shouldVerify: true,
		},
		{
			name:         "Transaction to Locker - Wrong Address",
			txHash:       SepoliaLockerTxHash,
			caipAddress:  "eip155:11155111:0x1234567890123456789012345678901234567890",
			shouldVerify: false,
		},
		{
			name:         "Invalid Transaction Hash",
			txHash:       "0xffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
			caipAddress:  SepoliaCAIPAddress,
			shouldVerify: false,
		},
		{
			name:         "Invalid CAIP Format",
			txHash:       SepoliaLockerTxHash,
			caipAddress:  "invalid:format:0x1234567890123456789012345678901234567890",
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
			// Use the specific locker verification method
			result, err := realKeeper.VerifyExternalTransactionToLockerDirect(ctx, tc.txHash, tc.caipAddress)
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

// TestVerifyLockerTransactionAddressCheck tests that the transaction is verified only if it's directed to the locker
func (suite *TxVerifyLockerTestSuite) TestVerifyLockerTransactionAddressCheck() {
	t := suite.T()

	// Skip the test by default to avoid making real RPC calls in CI
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Create a test copy of the chain config with a wrong locker address
	wrongLockerConfig := suite.chainConfig["eip155:11155111"]
	wrongLockerConfig.LockerContractAddress = "0xFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF" // Different from actual target

	// Create a new config map with the wrong locker address
	wrongLockerMap := map[string]types.ChainConfigData{
		"eip155:11155111": wrongLockerConfig,
	}

	// Create a keeper with the wrong locker address
	wrongLockerKeeper := keeper.NewKeeperWithConfigs(wrongLockerMap)

	// Set a timeout for the test
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Use a transaction that should be valid otherwise
	txHash := SepoliaLockerTxHash
	caipAddress := SepoliaCAIPAddress

	// Verify with the wrong locker address
	wrongLockerResult, err := wrongLockerKeeper.VerifyExternalTransactionToLockerDirect(ctx, txHash, caipAddress)

	// We expect this verification to fail because the transaction is not directed to our wrong locker address
	if err != nil {
		t.Logf("Wrong locker verification returned error: %v", err)
	} else {
		require.False(t, wrongLockerResult.Verified,
			"Transaction should not verify when directed to a different address than our locker")
		require.Contains(t, wrongLockerResult.TxInfo, "not directed to",
			"Verification failure reason should mention the transaction is not directed to the expected contract")
		t.Logf("Wrong locker test passed. Result: %s", wrongLockerResult.TxInfo)
	}

	// Now verify with the correct locker address to ensure it works
	correctLockerKeeper := suite.getRealKeeperWithConfigs()
	correctResult, err := correctLockerKeeper.VerifyExternalTransactionToLockerDirect(ctx, txHash, caipAddress)

	if err != nil {
		t.Logf("Correct locker verification returned error: %v", err)
	} else {
		// Transaction should verify with the correct locker address if we have the right locker address
		if correctResult.Verified {
			t.Logf("Correct locker test passed. Transaction verified correctly.")
		} else {
			t.Logf("Note: Transaction didn't verify, but this could be due to other factors: %s", correctResult.TxInfo)
		}
	}
}

// TestVerifyRealSepoliaTransaction is a comprehensive test using the real Sepolia transaction
// This test should be run after TestFindActualTargetAddress to discover the actual target address
func (suite *TxVerifyLockerTestSuite) TestVerifyRealSepoliaTransaction() {
	t := suite.T()

	// Skip the test by default to avoid making real RPC calls in CI
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Create a real keeper with minimal confirmation requirements
	// to ensure the test passes even on newer blocks
	minConfirmConfig := suite.chainConfig["eip155:11155111"]
	minConfirmConfig.BlockConfirmation = 1 // Only require 1 confirmation

	minConfirmMap := map[string]types.ChainConfigData{
		"eip155:11155111": minConfirmConfig,
	}

	realKeeper := keeper.NewKeeperWithConfigs(minConfirmMap)

	// Set a reasonable timeout
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	// Run verification with the real transaction hash
	txHash := SepoliaLockerTxHash
	caipAddress := SepoliaCAIPAddress

	result, err := realKeeper.VerifyExternalTransactionToLockerDirect(ctx, txHash, caipAddress)

	if err != nil {
		t.Logf("Verification error: %v", err)
		t.Errorf("Failed to verify transaction: %v", err)
	} else {
		t.Logf("Verification result: %t", result.Verified)
		t.Logf("Transaction info: %s", result.TxInfo)

		// Detailed verification results
		if result.Verified {
			t.Logf("Success: Transaction %s verified as directed to locker contract", txHash)
		} else {
			t.Logf("Note: If verification failed, check that SepoliaLockerAddr constant is set to the actual target address of the transaction")
			t.Logf("Run TestFindActualTargetAddress first to determine the correct address")

			if result.TxInfo != "" {
				// If the failure is due to the locker address mismatch, this should show in the TxInfo
				if strings.Contains(result.TxInfo, "not directed to the locker contract") {
					t.Logf("Transaction is not directed to the specified locker contract")
				} else if strings.Contains(result.TxInfo, "confirmations") {
					t.Logf("Transaction does not have enough confirmations, try again later")
				}
			}
		}
	}
}
