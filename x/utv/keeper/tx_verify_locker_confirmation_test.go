package keeper_test

import (
	"context"
	"testing"
	"time"

	"github.com/push-protocol/push-chain/x/utv/keeper"
	"github.com/push-protocol/push-chain/x/utv/types"
	"github.com/stretchr/testify/require"
)

// TestLockerTransactionConfirmations verifies that transactions require the specified number of confirmations
func TestLockerTransactionConfirmations(t *testing.T) {
	// Skip the test by default to avoid making real RPC calls in CI
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Create a real transaction hash that should be verified
	txHash := "0x14d7602b0c0902d8262ec2a21bf4fed0c0f8de6e0b663d5e9c3e7445293be2b2"
	caipAddress := "eip155:11155111:0xeCba9a32A9823f1cb00cdD8344Bf2D1d87a8dd97"

	// Create a chain config with the correct locker address but very high confirmation requirement
	highConfirmConfig := types.ChainConfigData{
		ChainId:               "11155111",
		ChainName:             "Ethereum Sepolia",
		CaipPrefix:            "eip155:11155111",
		LockerContractAddress: "0x871d4d00325F6D8D28Cad76Ff72A48Edd87E26AF", // Correct address
		UsdcAddress:           "0x1234567890AbCdEf1234567890AbCdEf12345678",
		PublicRpcUrl:          "https://ethereum-sepolia.publicnode.com",
		NetworkType:           types.NetworkTypeTestnet,
		VmType:                types.VmTypeEvm,
		BlockConfirmation:     100000, // Unrealistically high confirmation requirement
	}

	// Create a new config map with the high confirmation requirement
	highConfirmMap := map[string]types.ChainConfigData{
		"eip155:11155111": highConfirmConfig,
	}

	// Create a keeper with the high confirmation requirement
	highConfirmKeeper := keeper.NewKeeperWithConfigs(highConfirmMap)

	// Set a timeout for the test
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Try to verify the transaction with an unrealistically high confirmation requirement
	result, err := highConfirmKeeper.VerifyExternalTransactionToLockerDirect(ctx, txHash, caipAddress)

	// Verification should fail due to insufficient confirmations even though it's directed to the correct locker
	if err != nil {
		t.Logf("Error: %v", err)
	} else {
		// The transaction should not be verified
		require.False(t, result.Verified, "Transaction should not be verified with a high confirmation requirement")

		// The failure reason should mention confirmations
		require.Contains(t, result.TxInfo, "confirmation", "Verification failure should mention confirmations")
		t.Logf("Verification correctly failed due to high confirmation requirement: %s", result.TxInfo)

		// Now try with just 1 confirmation requirement
		lowConfirmConfig := highConfirmConfig
		lowConfirmConfig.BlockConfirmation = 1

		lowConfirmMap := map[string]types.ChainConfigData{
			"eip155:11155111": lowConfirmConfig,
		}

		lowConfirmKeeper := keeper.NewKeeperWithConfigs(lowConfirmMap)

		// This should work since we have enough confirmations
		lowResult, err := lowConfirmKeeper.VerifyExternalTransactionToLockerDirect(ctx, txHash, caipAddress)

		if err != nil {
			t.Fatalf("Error with low confirmation requirement: %v", err)
		}

		require.True(t, lowResult.Verified, "Transaction should be verified with low confirmation requirement")
		t.Logf("Transaction successfully verified with low confirmation requirement")
	}
}
