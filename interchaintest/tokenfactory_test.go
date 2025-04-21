package e2e

import (
	"context"
	"testing"

	"github.com/strangelove-ventures/interchaintest/v8"
	"github.com/strangelove-ventures/interchaintest/v8/chain/cosmos"
	"github.com/strangelove-ventures/interchaintest/v8/testreporter"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// TestTokenFactory verifies the functionality of the token factory module.
// This test ensures that:
// 1. A chain with token factory support can be properly initialized
// 2. A user can create a new token denomination
// 3. Tokens can be minted to the creator's address
// 4. Tokens can be minted to another user's address
// 5. Admin privileges can be transferred to another user
func TestTokenFactory(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

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
	ic := interchaintest.NewInterchain().AddChain(chain)

	// Build the interchain environment
	require.NoError(t, ic.Build(ctx, eRep, interchaintest.InterchainBuildOptions{
		TestName:         t.Name(),
		Client:           client,
		NetworkID:        network,
		SkipPathCreation: false,
	}))

	// Create and fund two test users
	users := interchaintest.GetAndFundTestUsers(t, ctx, "default", GenesisFundsAmount, chain, chain)
	user := users[0]
	user2 := users[1]

	// Get formatted addresses for both users
	uaddr := user.FormattedAddress()
	uaddr2 := user2.FormattedAddress()

	// Get a reference to the chain node for executing commands
	node := chain.GetNode()

	// Create a new token denomination with a creation fee of 5 million
	tfDenom, _, err := node.TokenFactoryCreateDenom(ctx, user, "ictestdenom", 5_000_000)
	t.Log("TF Denom: ", tfDenom)
	require.NoError(t, err)

	t.Run("Mint TF Denom to user", func(t *testing.T) {
		// Mint 100 tokens of the new denomination to the creator's address
		node.TokenFactoryMintDenom(ctx, user.FormattedAddress(), tfDenom, 100)

		// Verify the user's balance of the new token
		if balance, err := chain.GetBalance(ctx, uaddr, tfDenom); err != nil {
			t.Fatal(err)
		} else if balance.Int64() != 100 {
			t.Fatal("balance not 100")
		}
	})

	t.Run("Mint TF Denom to another user", func(t *testing.T) {
		// Mint 70 tokens of the new denomination to the second user's address
		node.TokenFactoryMintDenomTo(ctx, user.FormattedAddress(), tfDenom, 70, user2.FormattedAddress())

		// Verify the second user's balance of the new token
		if balance, err := chain.GetBalance(ctx, uaddr2, tfDenom); err != nil {
			t.Fatal(err)
		} else if balance.Int64() != 70 {
			t.Fatal("balance not 70")
		}
	})

	t.Run("Change admin to uaddr2", func(t *testing.T) {
		// Transfer admin privileges for the token to the second user
		_, err = node.TokenFactoryChangeAdmin(ctx, user.KeyName(), tfDenom, uaddr2)
		require.NoError(t, err)
	})

	t.Run("Validate new admin address", func(t *testing.T) {
		// Query the admin for the token and verify it's now the second user
		res, err := chain.TokenFactoryQueryAdmin(ctx, tfDenom)
		require.NoError(t, err)
		require.EqualValues(t, res.AuthorityMetadata.Admin, uaddr2, "admin not uaddr2. Did not properly transfer.")
	})

	// Clean up resources when the test completes
	t.Cleanup(func() {
		_ = ic.Close()
	})
}
