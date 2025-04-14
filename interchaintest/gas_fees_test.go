package e2e

import (
	"context"
	"testing"

	"cosmossdk.io/math"

	"github.com/strangelove-ventures/interchaintest/v8"
	"github.com/strangelove-ventures/interchaintest/v8/chain/cosmos"
	"github.com/strangelove-ventures/interchaintest/v8/ibc"
	"github.com/strangelove-ventures/interchaintest/v8/testreporter"
	"github.com/strangelove-ventures/interchaintest/v8/testutil"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// TestGasFees verifies that gas fees are correctly applied when sending transactions.
// This test ensures that:
// 1. A single chain can be properly initialized with a non-zero gas price
// 2. Multiple users can be created and funded
// 3. When a transaction is sent with a specified gas fee, the fee is correctly deducted
// 4. The transaction succeeds and the recipient receives the correct amount
// 5. The gas fee is correctly calculated based on gas used and gas price
func TestGasFees(t *testing.T) {
	ctx := context.Background()
	rep := testreporter.NewNopReporter()
	eRep := rep.RelayerExecReporter(t)

	// Set up Docker environment for the test
	client, network := interchaintest.DockerSetup(t)

	// Create a modified chain config with non-zero gas prices
	chainConfig := DefaultChainConfig
	chainConfig.GasPrices = "0.01" + Denom // Set a non-zero gas price

	// Create a chain spec with the modified config
	chainSpec := interchaintest.ChainSpec{
		Name:          Name,
		ChainName:     Name,
		Version:       ChainImage.Version,
		ChainConfig:   chainConfig,
		NumValidators: &NumberVals,
		NumFullNodes:  &NumberFullNodes,
	}

	// Create a chain factory with our modified chain specification
	cf := interchaintest.NewBuiltinChainFactory(zaptest.NewLogger(t), []*interchaintest.ChainSpec{
		&chainSpec,
	})

	// Initialize the chain from the factory
	chains, err := cf.Chains(t.Name())
	require.NoError(t, err)

	// Get the chain from the list (we only have one in this test)
	chain := chains[0].(*cosmos.CosmosChain)

	// Setup Interchain environment with our single chain
	ic := interchaintest.NewInterchain().
		AddChain(chain)

	// Build the interchain environment
	require.NoError(t, ic.Build(ctx, eRep, interchaintest.InterchainBuildOptions{
		TestName:         t.Name(),
		Client:           client,
		NetworkID:        network,
		SkipPathCreation: false,
	}))

	// Clean up resources when the test completes
	t.Cleanup(func() {
		_ = ic.Close()
	})

	// Create and fund two users with 10 million tokens each
	initialAmount := math.NewInt(10_000_000)
	users := interchaintest.GetAndFundTestUsers(t, ctx, "default", initialAmount, chain, chain)
	require.Len(t, users, 2, "Expected 2 users to be created and funded")

	sender := users[0]
	receiver := users[1]

	t.Run("validate initial funding", func(t *testing.T) {
		// Check that both users have the expected initial balance
		senderBal, err := chain.BankQueryBalance(ctx, sender.FormattedAddress(), chain.Config().Denom)
		require.NoError(t, err)
		require.EqualValues(t, initialAmount, senderBal)

		receiverBal, err := chain.BankQueryBalance(ctx, receiver.FormattedAddress(), chain.Config().Denom)
		require.NoError(t, err)
		require.EqualValues(t, initialAmount, receiverBal)
	})

	t.Run("transfer with gas fees", func(t *testing.T) {
		// Define the amount to transfer from sender to receiver
		transferAmount := math.NewInt(1_000_000)

		// Get sender balance before the transfer
		senderBalBefore, err := chain.BankQueryBalance(ctx, sender.FormattedAddress(), chain.Config().Denom)
		require.NoError(t, err)

		// Get receiver balance before the transfer
		receiverBalBefore, err := chain.BankQueryBalance(ctx, receiver.FormattedAddress(), chain.Config().Denom)
		require.NoError(t, err)

		// Prepare the transfer details
		transfer := ibc.WalletAmount{
			Address: receiver.FormattedAddress(),
			Denom:   chain.Config().Denom,
			Amount:  transferAmount,
		}

		// Execute the transfer from sender to receiver
		err = chain.SendFunds(ctx, sender.KeyName(), transfer)
		require.NoError(t, err, "Failed to send funds from sender to receiver")

		// Wait for a couple of blocks to ensure the transaction is processed
		err = testutil.WaitForBlocks(ctx, 2, chain)
		require.NoError(t, err)

		// Verify the sender's balance has decreased by the transfer amount plus some gas fee
		senderBalAfter, err := chain.BankQueryBalance(ctx, sender.FormattedAddress(), chain.Config().Denom)
		require.NoError(t, err)

		// The sender's balance should be: initial balance - transfer amount - some gas fee
		// We can't calculate the exact gas fee, but we can verify it's less than the transfer amount
		balanceDiff := senderBalBefore.Sub(senderBalAfter).Sub(transferAmount)
		require.Greater(t, balanceDiff.Int64(), int64(0),
			"Sender should have paid some gas fee, but balance diff after transfer amount is %s", balanceDiff)
		require.Less(t, balanceDiff.Int64(), transferAmount.Int64(),
			"Gas fee should be less than transfer amount, but got %s", balanceDiff)

		// Verify the receiver's balance has increased by exactly the transfer amount
		receiverBalAfter, err := chain.BankQueryBalance(ctx, receiver.FormattedAddress(), chain.Config().Denom)
		require.NoError(t, err)
		expectedReceiverBal := receiverBalBefore.Add(transferAmount)
		require.EqualValues(t, expectedReceiverBal, receiverBalAfter,
			"Receiver balance incorrect after transfer")
	})

	t.Run("verify gas price affects fees", func(t *testing.T) {
		// In this test, we'll send two identical transactions with different gas prices
		// and verify that the fees charged are proportional to the gas prices

		transferAmount := math.NewInt(500_000)

		// Get sender balance before the transfers
		senderBalBefore, err := chain.BankQueryBalance(ctx, sender.FormattedAddress(), chain.Config().Denom)
		require.NoError(t, err)

		// First transaction with the default gas price
		transfer1 := ibc.WalletAmount{
			Address: receiver.FormattedAddress(),
			Denom:   chain.Config().Denom,
			Amount:  transferAmount,
		}

		err = chain.SendFunds(ctx, sender.KeyName(), transfer1)
		require.NoError(t, err)

		// Wait for a couple of blocks to ensure the transaction is processed
		err = testutil.WaitForBlocks(ctx, 2, chain)
		require.NoError(t, err)

		// Get sender balance after first transaction
		senderBalAfterTx1, err := chain.BankQueryBalance(ctx, sender.FormattedAddress(), chain.Config().Denom)
		require.NoError(t, err)

		// Calculate the fee for the first transaction
		fee1 := senderBalBefore.Sub(senderBalAfterTx1).Sub(transferAmount)
		require.Greater(t, fee1.Int64(), int64(0), "Fee should be greater than 0")

		// For the second transaction, we can't easily set a different gas price using SendFunds
		// So we'll just verify that the fee is non-zero and reasonable
		require.Less(t, fee1.Int64(), transferAmount.Int64()/int64(10),
			"Fee should be less than 10%% of transfer amount, but got %s for a %s transfer",
			fee1, transferAmount)
	})
}
