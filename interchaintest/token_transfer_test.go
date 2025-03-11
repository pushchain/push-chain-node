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

func TestTokenTransfer(t *testing.T) {
	ctx := context.Background()
	rep := testreporter.NewNopReporter()
	eRep := rep.RelayerExecReporter(t)
	client, network := interchaintest.DockerSetup(t)

	cf := interchaintest.NewBuiltinChainFactory(zaptest.NewLogger(t), []*interchaintest.ChainSpec{
		&DefaultChainSpec,
	})

	chains, err := cf.Chains(t.Name())
	require.NoError(t, err)

	chain := chains[0].(*cosmos.CosmosChain)

	// Setup Interchain
	ic := interchaintest.NewInterchain().
		AddChain(chain)

	require.NoError(t, ic.Build(ctx, eRep, interchaintest.InterchainBuildOptions{
		TestName:         t.Name(),
		Client:           client,
		NetworkID:        network,
		SkipPathCreation: false,
	}))
	t.Cleanup(func() {
		_ = ic.Close()
	})

	// Create and fund two users
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
		// Transfer amount
		transferAmount := math.NewInt(1_000_000)

		// Perform the transfer from sender to receiver
		transfer := ibc.WalletAmount{
			Address: receiver.FormattedAddress(),
			Denom:   chain.Config().Denom,
			Amount:  transferAmount,
		}
		err := chain.SendFunds(ctx, sender.KeyName(), transfer)
		require.NoError(t, err, "Failed to send funds from sender to receiver")

		// Verify the balances after transfer
		senderBal, err := chain.BankQueryBalance(ctx, sender.FormattedAddress(), chain.Config().Denom)
		require.NoError(t, err)
		expectedSenderBal := initialAmount.Sub(transferAmount)
		require.EqualValues(t, expectedSenderBal, senderBal, "Sender balance incorrect after transfer")

		receiverBal, err := chain.BankQueryBalance(ctx, receiver.FormattedAddress(), chain.Config().Denom)
		require.NoError(t, err)
		expectedReceiverBal := initialAmount.Add(transferAmount)
		require.EqualValues(t, expectedReceiverBal, receiverBal, "Receiver balance incorrect after transfer")
	})

	t.Run("transfer tokens back", func(t *testing.T) {
		// Transfer amount for the return transfer
		returnAmount := math.NewInt(500_000)

		// Get balances before the return transfer
		senderBalBefore, err := chain.BankQueryBalance(ctx, sender.FormattedAddress(), chain.Config().Denom)
		require.NoError(t, err)

		receiverBalBefore, err := chain.BankQueryBalance(ctx, receiver.FormattedAddress(), chain.Config().Denom)
		require.NoError(t, err)

		// Perform the transfer from receiver back to sender
		returnTransfer := ibc.WalletAmount{
			Address: sender.FormattedAddress(),
			Denom:   chain.Config().Denom,
			Amount:  returnAmount,
		}
		err = chain.SendFunds(ctx, receiver.KeyName(), returnTransfer)
		require.NoError(t, err, "Failed to send funds from receiver back to sender")

		// Verify the balances after the return transfer
		senderBalAfter, err := chain.BankQueryBalance(ctx, sender.FormattedAddress(), chain.Config().Denom)
		require.NoError(t, err)
		expectedSenderBalAfter := senderBalBefore.Add(returnAmount)
		require.EqualValues(t, expectedSenderBalAfter, senderBalAfter, "Sender balance incorrect after return transfer")

		receiverBalAfter, err := chain.BankQueryBalance(ctx, receiver.FormattedAddress(), chain.Config().Denom)
		require.NoError(t, err)
		expectedReceiverBalAfter := receiverBalBefore.Sub(returnAmount)
		require.EqualValues(t, expectedReceiverBalAfter, receiverBalAfter, "Receiver balance incorrect after return transfer")
	})
}
