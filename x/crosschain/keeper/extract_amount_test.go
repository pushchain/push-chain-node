package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	
	sdkmath "cosmossdk.io/math"
	"github.com/push-protocol/push-chain/x/crosschain/keeper"
)

func TestExtractAmountFromTransactionLogs(t *testing.T) {
	// Test case for a specific real transaction from Sepolia
	// Transaction: 0x14d7602b0c0902d8262ec2a21bf4fed0c0f8de6e0b663d5e9c3e7445293be2b2
	t.Run("real transaction with FundsAdded event", func(t *testing.T) {
		// This is a simplified transaction receipt JSON with just the relevant parts
		txInfoJSON := `{
			"logs": [
				{
					"address": "0x57235d27c8247cfe0e39248c9c9f22bd6eb054e1",
					"topics": [
						"0xddcd6ef7998ae51b4ead4e9aa669a7d5ff30af88eddaa5062c91b08153da07c0",
						"0x000000000000000000000000ecba9a32a9823f1cb00cdd8344bf2d1d87a8dd97"
					],
					"data": "0x0000000000000000000000000000000000000000000000000000000ddc1e02180000000000000000000000000000000000000000000000000000000000000000"
				}
			]
		}`

	// The expected amount in decimal from the transaction is 59,527,528,984
	expectedAmount := sdkmath.NewIntFromUint64(59527528984)
		
		// We use the default EVM VM type (1)
		vmType := uint8(1)
		
		// Call the function with the FundsAdded event topic signature
		amount, err := keeper.ExtractAmountFromTransactionLogs(
			txInfoJSON, 
			"0xddcd6ef7998ae51b4ead4e9aa669a7d5ff30af88eddaa5062c91b08153da07c0",
			vmType,
		)
		
		require.NoError(t, err)
		require.True(t, amount.Equal(expectedAmount), "Expected %s but got %s", expectedAmount.String(), amount.String())
	})

	t.Run("transaction with indexed FundsAdded event parameter", func(t *testing.T) {
		// This is a modified transaction where the amount is in the topics array
		txInfoJSON := `{
			"logs": [
				{
					"address": "0x57235d27c8247cfe0e39248c9c9f22bd6eb054e1",
					"topics": [
						"0xddcd6ef7998ae51b4ead4e9aa669a7d5ff30af88eddaa5062c91b08153da07c0",
						"0x000000000000000000000000ecba9a32a9823f1cb00cdd8344bf2d1d87a8dd97",
						"0x0000000000000000000000000000000000000000000000000000000ddc1e0218"
					],
					"data": "0x0000000000000000000000000000000000000000000000000000000000000000"
				}
			]
		}`

	// The expected amount in decimal is 59,527,528,984
	expectedAmount := sdkmath.NewIntFromUint64(59527528984)
		
		// We use the default EVM VM type (1)
		vmType := uint8(1)
		
		// Call the function with the FundsAdded event topic signature
		amount, err := keeper.ExtractAmountFromTransactionLogs(
			txInfoJSON, 
			"0xddcd6ef7998ae51b4ead4e9aa669a7d5ff30af88eddaa5062c91b08153da07c0",
			vmType,
		)
		
		require.NoError(t, err)
		require.True(t, amount.Equal(expectedAmount), "Expected %s but got %s", expectedAmount.String(), amount.String())
	})

	t.Run("transaction with different event topic", func(t *testing.T) {
		// This is a transaction with a different event topic
		txInfoJSON := `{
			"logs": [
				{
					"address": "0x57235d27c8247cfe0e39248c9c9f22bd6eb054e1",
					"topics": [
						"0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef",
						"0x000000000000000000000000ecba9a32a9823f1cb00cdd8344bf2d1d87a8dd97"
					],
					"data": "0x0000000000000000000000000000000000000000000000000000000ddc1e0218"
				}
			]
		}`

		// We use the default EVM VM type (1)
		vmType := uint8(1)
		
		// Call the function with the FundsAdded event topic signature
		amount, err := keeper.ExtractAmountFromTransactionLogs(
			txInfoJSON, 
			"0xddcd6ef7998ae51b4ead4e9aa669a7d5ff30af88eddaa5062c91b08153da07c0",
			vmType,
		)
		
		// We expect an error because the event is not found
		require.Error(t, err)
		require.Contains(t, err.Error(), "FundsAdded event")
		require.True(t, amount.IsZero())
	})

	t.Run("transaction with no logs", func(t *testing.T) {
		// This is a transaction with no logs
		txInfoJSON := `{
			"logs": []
		}`

		// We use the default EVM VM type (1)
		vmType := uint8(1)
		
		// Call the function with the FundsAdded event topic signature
		amount, err := keeper.ExtractAmountFromTransactionLogs(
			txInfoJSON, 
			"0xddcd6ef7998ae51b4ead4e9aa669a7d5ff30af88eddaa5062c91b08153da07c0",
			vmType,
		)
		
		// We expect an error because there are no logs
		require.Error(t, err)
		require.Contains(t, err.Error(), "no logs found")
		require.True(t, amount.IsZero())
	})

	t.Run("invalid transaction JSON", func(t *testing.T) {
		// This is an invalid JSON
		txInfoJSON := `{
			"logs": [
				{
					"invalid JSON"
				}
			]
		}`

		// We use the default EVM VM type (1)
		vmType := uint8(1)
		
		// Call the function with the FundsAdded event topic signature
		amount, err := keeper.ExtractAmountFromTransactionLogs(
			txInfoJSON, 
			"0xddcd6ef7998ae51b4ead4e9aa669a7d5ff30af88eddaa5062c91b08153da07c0",
			vmType,
		)
		
		// We expect an error because the JSON is invalid
		require.Error(t, err)
		require.True(t, amount.IsZero())
	})

	t.Run("unsupported VM type", func(t *testing.T) {
		// This is a transaction with a valid FundsAdded event
		txInfoJSON := `{
			"logs": [
				{
					"address": "0x57235d27c8247cfe0e39248c9c9f22bd6eb054e1",
					"topics": [
						"0xddcd6ef7998ae51b4ead4e9aa669a7d5ff30af88eddaa5062c91b08153da07c0",
						"0x000000000000000000000000ecba9a32a9823f1cb00cdd8344bf2d1d87a8dd97"
					],
					"data": "0x0000000000000000000000000000000000000000000000000000000ddc1e02180000000000000000000000000000000000000000000000000000000000000000"
				}
			]
		}`

		// We use an unsupported VM type (2 for Solana VM)
		vmType := uint8(2)
		
		// Call the function with the FundsAdded event topic signature
		amount, err := keeper.ExtractAmountFromTransactionLogs(
			txInfoJSON, 
			"0xddcd6ef7998ae51b4ead4e9aa669a7d5ff30af88eddaa5062c91b08153da07c0",
			vmType,
		)
		
		// We expect an error because the VM type is not supported
		require.Error(t, err)
		require.Contains(t, err.Error(), "not yet implemented")
		require.True(t, amount.IsZero())
	})
}
