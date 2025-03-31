package e2e

import (
	"context"
	"fmt"
	"testing"

	"cosmossdk.io/math"
	"github.com/strangelove-ventures/interchaintest/v8"
	"github.com/strangelove-ventures/interchaintest/v8/chain/cosmos"
	"github.com/strangelove-ventures/interchaintest/v8/ibc"
	interchaintestrelayer "github.com/strangelove-ventures/interchaintest/v8/relayer"
	"github.com/strangelove-ventures/interchaintest/v8/testreporter"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest"
)

// TestIBCRateLimit verifies that the IBC rate limiting functionality works correctly.
// This test ensures that:
// 1. Two chains can be properly initialized and connected via IBC
// 2. The rate limit module can be configured to blacklist specific denominations
// 3. Transfers of blacklisted denominations are properly rejected
// 4. The error message correctly indicates that the denomination is blacklisted
func TestIBCRateLimit(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}

	t.Parallel()
	ctx := context.Background()
	rep := testreporter.NewNopReporter()
	eRep := rep.RelayerExecReporter(t)
	// Set up Docker environment for the test
	client, network := interchaintest.DockerSetup(t)

	// Configure the first chain with the chain's native denom blacklisted in the rate limit module
	cs := &DefaultChainSpec
	cs.ModifyGenesis = cosmos.ModifyGenesis([]cosmos.GenesisKV{cosmos.NewGenesisKV("app_state.ratelimit.blacklisted_denoms", []string{cs.Denom})})

	// Create a chain factory with two chain specifications
	cf := interchaintest.NewBuiltinChainFactory(zaptest.NewLogger(t), []*interchaintest.ChainSpec{
		cs,
		&SecondDefaultChainSpec,
	})

	// Initialize the chains from the factory
	chains, err := cf.Chains(t.Name())
	require.NoError(t, err)

	// Get references to both chains
	chain := chains[0].(*cosmos.CosmosChain)
	secondary := chains[1].(*cosmos.CosmosChain)

	// Set up the relayer for IBC communication between chains
	r := interchaintest.NewBuiltinRelayerFactory(
		ibc.CosmosRly,
		zaptest.NewLogger(t, zaptest.Level(zapcore.DebugLevel)),
		interchaintestrelayer.CustomDockerImage(RelayerRepo, RelayerVersion, "100:1000"),
		interchaintestrelayer.StartupFlags("--processor", "events", "--block-history", "200"),
	).Build(t, client, network)

	// Create the interchain environment with both chains and the relayer
	ic := interchaintest.NewInterchain().
		AddChain(chain).
		AddChain(secondary).
		AddRelayer(r, "relayer")

	// Add an IBC link between the two chains
	ic = ic.AddLink(interchaintest.InterchainLink{
		Chain1:  chain,
		Chain2:  secondary,
		Relayer: r,
		Path:    ibcPath,
	})

	// Build the interchain environment
	require.NoError(t, ic.Build(ctx, eRep, interchaintest.InterchainBuildOptions{
		TestName:         t.Name(),
		Client:           client,
		NetworkID:        network,
		SkipPathCreation: false,
	}))

	// Create and fund test users on both chains
	fundAmount := math.NewInt(10_000_000)
	users := interchaintest.GetAndFundTestUsers(t, ctx, "default", fundAmount, chain, secondary)
	userA, userB := users[0], users[1]

	// Verify initial balance of userA
	userAInitial, err := chain.GetBalance(ctx, userA.FormattedAddress(), chain.Config().Denom)
	fmt.Println("userAInitial", userAInitial)
	require.NoError(t, err)
	require.True(t, userAInitial.Equal(fundAmount))

	// Get the IBC channel ID for chainA
	aInfo, err := r.GetChannels(ctx, eRep, chain.Config().ChainID)
	require.NoError(t, err)
	aChannelID, err := getTransferChannel(aInfo)
	require.NoError(t, err)
	fmt.Println("aChannelID", aChannelID)

	// Prepare the IBC transfer from chainA to chainB
	amountToSend := math.NewInt(1_000_000)
	dstAddress := userB.FormattedAddress()
	transfer := ibc.WalletAmount{
		Address: dstAddress,
		Denom:   chain.Config().Denom,
		Amount:  amountToSend,
	}

	// Attempt the IBC transfer and verify it fails due to the blacklisted denom
	_, err = chain.SendIBCTransfer(ctx, aChannelID, userA.KeyName(), transfer, ibc.TransferOptions{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "denom is blacklisted")
}
