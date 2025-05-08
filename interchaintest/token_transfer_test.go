package e2e

import (
	"context"
	"testing"

	"cosmossdk.io/math"

	"github.com/strangelove-ventures/interchaintest/v8"
	"github.com/strangelove-ventures/interchaintest/v8/chain/cosmos"
	"github.com/strangelove-ventures/interchaintest/v8/ibc"
	"github.com/strangelove-ventures/interchaintest/v8/testreporter"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// TestTokenTransfer verifies that tokens can be transferred between users on the same chain.
// This test ensures that:
// 1. A single chain can be properly initialized
// 2. Multiple users can be created and funded
// 3. Tokens can be transferred from one user to another
// 4. Balances are correctly updated after transfers
// 5. Tokens can be transferred back to the original sender
func TestTokenTransfer(t *testing.T) {
	ctx := context.Background()
	rep := testreporter.NewNopReporter()
	eRep := rep.RelayerExecReporter(t)
	// Set up Docker environment for the test
	client, network := interchaintest.DockerSetup(t)

	// Create a chain factory with our default chain specification
	cf := interchaintest.NewBuiltinChainFactory(zaptest.NewLogger(t), []*interchaintest.ChainSpec{
		&DefaultChainSpec,
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

	t.Run("transfer tokens between users", func(t *testing.T) {
		// Define the amount to transfer from sender to receiver
		transferAmount := math.NewInt(1_000_000)

		// Prepare the transfer details
		transfer := ibc.WalletAmount{
			Address: receiver.FormattedAddress(),
			Denom:   chain.Config().Denom,
			Amount:  transferAmount,
		}

		// Execute the transfer from sender to receiver
		err := chain.SendFunds(ctx, sender.KeyName(), transfer)
		require.NoError(t, err, "Failed to send funds from sender to receiver")

		// Verify the sender's balance has decreased by the transfer amount
		senderBal, err := chain.BankQueryBalance(ctx, sender.FormattedAddress(), chain.Config().Denom)
		require.NoError(t, err)
		expectedSenderBal := initialAmount.Sub(transferAmount)
		require.EqualValues(t, expectedSenderBal, senderBal, "Sender balance incorrect after transfer")

		// Verify the receiver's balance has increased by the transfer amount
		receiverBal, err := chain.BankQueryBalance(ctx, receiver.FormattedAddress(), chain.Config().Denom)
		require.NoError(t, err)
		expectedReceiverBal := initialAmount.Add(transferAmount)
		require.EqualValues(t, expectedReceiverBal, receiverBal, "Receiver balance incorrect after transfer")
	})

	t.Run("transfer tokens back", func(t *testing.T) {
		// Define the amount to transfer back from receiver to sender
		returnAmount := math.NewInt(500_000)

		// Get balances before the return transfer
		senderBalBefore, err := chain.BankQueryBalance(ctx, sender.FormattedAddress(), chain.Config().Denom)
		require.NoError(t, err)

		receiverBalBefore, err := chain.BankQueryBalance(ctx, receiver.FormattedAddress(), chain.Config().Denom)
		require.NoError(t, err)

		// Prepare the return transfer details
		returnTransfer := ibc.WalletAmount{
			Address: sender.FormattedAddress(),
			Denom:   chain.Config().Denom,
			Amount:  returnAmount,
		}

		// Execute the transfer from receiver back to sender
		err = chain.SendFunds(ctx, receiver.KeyName(), returnTransfer)
		require.NoError(t, err, "Failed to send funds from receiver back to sender")

		// Verify the sender's balance has increased by the return amount
		senderBalAfter, err := chain.BankQueryBalance(ctx, sender.FormattedAddress(), chain.Config().Denom)
		require.NoError(t, err)
		expectedSenderBalAfter := senderBalBefore.Add(returnAmount)
		require.EqualValues(t, expectedSenderBalAfter, senderBalAfter, "Sender balance incorrect after return transfer")

		// Verify the receiver's balance has decreased by the return amount
		receiverBalAfter, err := chain.BankQueryBalance(ctx, receiver.FormattedAddress(), chain.Config().Denom)
		require.NoError(t, err)
		expectedReceiverBalAfter := receiverBalBefore.Sub(returnAmount)
		require.EqualValues(t, expectedReceiverBalAfter, receiverBalAfter, "Receiver balance incorrect after return transfer")
	})
}
