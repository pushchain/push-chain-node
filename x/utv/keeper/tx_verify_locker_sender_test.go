package keeper_test

import (
	"context"
	"testing"
	"time"

	"github.com/push-protocol/push-chain/x/utv/keeper"
	"github.com/push-protocol/push-chain/x/utv/types"
	"github.com/stretchr/testify/require"
)

// TestLockerTransactionSenderVerification tests that the transaction sender is correctly verified
func TestLockerTransactionSenderVerification(t *testing.T) {
	// Skip the test by default to avoid making real RPC calls in CI
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Create a real transaction hash and the sender address
	txHash := "0x14d7602b0c0902d8262ec2a21bf4fed0c0f8de6e0b663d5e9c3e7445293be2b2"
	correctSender := "0xeCba9a32A9823f1cb00cdD8344Bf2D1d87a8dd97"
	wrongSender := "0x1234567890123456789012345678901234567890"

	// Create CAIPs with correct and wrong sender addresses
	correctCAIP := "eip155:11155111:" + correctSender
	wrongCAIP := "eip155:11155111:" + wrongSender

	// Create chain config with the correct locker address
	config := types.ChainConfigData{
		ChainId:               "11155111",
		ChainName:             "Ethereum Sepolia",
		CaipPrefix:            "eip155:11155111",
		LockerContractAddress: "0x57235d27c8247CFE0E39248c9c9F22BD6EB054e1", // Correct address
		UsdcAddress:           "0x1234567890AbCdEf1234567890AbCdEf12345678",
		PublicRpcUrl:          "https://ethereum-sepolia.publicnode.com",
		NetworkType:           types.NetworkTypeTestnet,
		VmType:                types.VmTypeEvm,
		BlockConfirmation:     1, // Minimal confirmation requirement
	}

	// Create a config map with the locker config
	configMap := map[string]types.ChainConfigData{
		"eip155:11155111": config,
	}

	// Create a keeper with this config
	testKeeper := keeper.NewKeeperWithConfigs(configMap)

	// Set a timeout for the test
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Test 1: Verify with correct sender address
	t.Run("CorrectSender", func(t *testing.T) {
		result, err := testKeeper.VerifyExternalTransactionToLockerDirect(ctx, txHash, correctCAIP)

		if err != nil {
			t.Fatalf("Error verifying correct sender: %v", err)
		}

		require.True(t, result.Verified, "Transaction should be verified with correct sender")
		t.Logf("Transaction successfully verified with correct sender")
	})

	// Test 2: Verify with wrong sender address
	t.Run("WrongSender", func(t *testing.T) {
		result, err := testKeeper.VerifyExternalTransactionToLockerDirect(ctx, txHash, wrongCAIP)

		if err != nil {
			t.Logf("Expected error verifying wrong sender: %v", err)
		} else {
			require.False(t, result.Verified, "Transaction should not be verified with wrong sender")
			require.Contains(t, result.TxInfo, "from", "Verification failure should mention the sender address")
			require.Contains(t, result.TxInfo, wrongSender, "Verification failure should mention the wrong sender address")
			t.Logf("Verification correctly failed with wrong sender: %s", result.TxInfo)
		}
	})
}
