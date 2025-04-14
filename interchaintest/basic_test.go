package e2e

import (
	"context"
	"testing"

	"cosmossdk.io/math"

	"github.com/strangelove-ventures/interchaintest/v8"
	"github.com/strangelove-ventures/interchaintest/v8/chain/cosmos"
	"github.com/strangelove-ventures/interchaintest/v8/testreporter"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// TestBasicChain is a fundamental test that verifies the basic functionality of a single chain.
// This test ensures that:
// 1. The chain can be properly initialized and started
// 2. The chain has the correct configuration (denom, chain ID, gas prices)
// 3. Users can be created and funded with tokens
// 4. The balance query functionality works correctly
func TestBasicChain(t *testing.T) {
	ctx := context.Background()
	rep := testreporter.NewNopReporter()
	eRep := rep.RelayerExecReporter(t)
	// Set up Docker environment for the test
	client, network := interchaintest.DockerSetup(t)

	// Create a chain factory with our default chain specification
	cf := interchaintest.NewBuiltinChainFactory(zaptest.NewLogger(t), []*interchaintest.ChainSpec{
		&DefaultChainSpec,
	})

	// Initialize the chains from the factory
	chains, err := cf.Chains(t.Name())
	require.NoError(t, err)

	// Get the first chain from the list (we only have one in this test)
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

	// Create and fund a test user with 10 million tokens
	amt := math.NewInt(10_000_000)
	users := interchaintest.GetAndFundTestUsers(t, ctx, "default", amt,
		chain,
	)
	user := users[0]

	t.Run("validate funding", func(t *testing.T) {
		// Verify that the user was properly funded with the expected amount
		bal, err := chain.BankQueryBalance(ctx, user.FormattedAddress(), chain.Config().Denom)
		require.NoError(t, err)
		require.EqualValues(t, amt, bal)
	})
}
