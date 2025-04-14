package e2e

import (
	"context"
	"testing"
	"time"

	"cosmossdk.io/math"
	"github.com/strangelove-ventures/interchaintest/v8"
	"github.com/strangelove-ventures/interchaintest/v8/chain/cosmos"
	"github.com/strangelove-ventures/interchaintest/v8/testreporter"
	"github.com/strangelove-ventures/interchaintest/v8/testutil"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// TestBlockTimeConfiguration verifies that the blockchain respects a custom block time parameter.
// This test ensures that:
// 1. The chain can be properly initialized with a custom block time
// 2. Blocks are produced at approximately the configured interval
// 3. The actual block production time is close to the configured block time
func TestBlockTimeConfiguration(t *testing.T) {
	ctx := context.Background()
	rep := testreporter.NewNopReporter()
	eRep := rep.RelayerExecReporter(t)

	// Set up Docker environment for the test
	client, network := interchaintest.DockerSetup(t)

	// Set a custom block time (2 seconds instead of default)
	customBlockTime := 2 * time.Second

	// Create a modified chain spec with custom block time
	// Note: We need to modify the genesis to set the block time
	chainSpec := DefaultChainSpec

	// Create a copy of the default genesis and add our custom block time
	genesisKVs := make([]cosmos.GenesisKV, len(DefaultGenesis))
	copy(genesisKVs, DefaultGenesis)

	// Add the block time configuration to the genesis
	genesisKVs = append(genesisKVs,
		cosmos.NewGenesisKV("consensus.params.block.time_iota_ms", "2000")) // 2000ms = 2s

	// Set the modified genesis
	chainSpec.ModifyGenesis = cosmos.ModifyGenesis(genesisKVs)

	// Create a chain factory with our modified chain specification
	cf := interchaintest.NewBuiltinChainFactory(zaptest.NewLogger(t), []*interchaintest.ChainSpec{
		&chainSpec,
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
		SkipPathCreation: true,
	}))

	// Clean up resources when the test completes
	t.Cleanup(func() {
		_ = ic.Close()
	})

	// Create and fund a test user with 10 million tokens
	amt := math.NewInt(10_000_000)
	users := interchaintest.GetAndFundTestUsers(t, ctx, "default", amt, chain)
	user := users[0]

	// Verify that the user was properly funded with the expected amount
	bal, err := chain.BankQueryBalance(ctx, user.FormattedAddress(), chain.Config().Denom)
	require.NoError(t, err)
	require.EqualValues(t, amt, bal)

	// Get the current block height
	height, err := chain.Height(ctx)
	require.NoError(t, err)
	startHeight := height

	// Record the start time
	startTime := time.Now()

	// Wait for a specific number of blocks to be produced
	numBlocksToWait := 5
	err = testutil.WaitForBlocks(ctx, numBlocksToWait, chain)
	require.NoError(t, err)

	// Get the new block height
	height, err = chain.Height(ctx)
	require.NoError(t, err)
	endHeight := height

	// Calculate the elapsed time
	elapsedTime := time.Since(startTime)

	// Calculate the actual number of blocks produced
	blocksProduced := endHeight - startHeight

	// Calculate the average time per block
	avgTimePerBlock := elapsedTime / time.Duration(blocksProduced)

	// Verify that the average block time is close to the configured block time
	// Allow for a 50% margin of error to account for network delays and other factors
	maxAllowedDeviation := time.Duration(float64(customBlockTime) * 0.5)

	t.Logf("Configured block time: %v", customBlockTime)
	t.Logf("Actual average block time: %v", avgTimePerBlock)
	t.Logf("Blocks produced: %d", blocksProduced)
	t.Logf("Elapsed time: %v", elapsedTime)

	// Check if the average block time is within the acceptable range
	require.InDelta(t, customBlockTime.Seconds(), avgTimePerBlock.Seconds(),
		maxAllowedDeviation.Seconds(),
		"Average block time (%v) is not close enough to configured block time (%v)",
		avgTimePerBlock, customBlockTime)
}
