package e2e

import (
	"context"
	"testing"

	"cosmossdk.io/math"
	transfertypes "github.com/cosmos/ibc-go/v8/modules/apps/transfer/types"
	"github.com/strangelove-ventures/interchaintest/v8"
	"github.com/strangelove-ventures/interchaintest/v8/chain/cosmos"
	"github.com/strangelove-ventures/interchaintest/v8/ibc"
	interchaintestrelayer "github.com/strangelove-ventures/interchaintest/v8/relayer"
	"github.com/strangelove-ventures/interchaintest/v8/testreporter"
	"github.com/strangelove-ventures/interchaintest/v8/testutil"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest"
)

const (
	ibcPath = "ibc-path"
)

// TestIBCBasic verifies the Inter-Blockchain Communication (IBC) functionality between two chains.
// This test ensures that:
// 1. Two separate chains can be initialized and connected via IBC
// 2. A relayer can be set up to facilitate communication between the chains
// 3. Tokens can be transferred from one chain to another via IBC
// 4. The balances are correctly updated on both chains after the transfer
func TestIBCBasic(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	rep := testreporter.NewNopReporter()
	eRep := rep.RelayerExecReporter(t)
	// Set up Docker environment for the test
	client, network := interchaintest.DockerSetup(t)

	// Configure the first chain with rate limit blacklisted denoms set to empty
	cs := &DefaultChainSpec
	cs.ModifyGenesis = cosmos.ModifyGenesis([]cosmos.GenesisKV{cosmos.NewGenesisKV("app_state.ratelimit.blacklisted_denoms", []string{})})

	// Create a chain factory with two chain specifications
	cf := interchaintest.NewBuiltinChainFactory(zaptest.NewLogger(t), []*interchaintest.ChainSpec{
		cs,
		&SecondDefaultChainSpec,
	})

	// Initialize the chains from the factory
	chains, err := cf.Chains(t.Name())
	require.NoError(t, err)

	// Get references to both chains
	chainA, chainB := chains[0].(*cosmos.CosmosChain), chains[1].(*cosmos.CosmosChain)

	// Set up the relayer for IBC communication between chains
	r := interchaintest.NewBuiltinRelayerFactory(
		ibc.CosmosRly,
		zaptest.NewLogger(t, zaptest.Level(zapcore.DebugLevel)),
		interchaintestrelayer.CustomDockerImage(RelayerRepo, RelayerVersion, "100:1000"),
		interchaintestrelayer.StartupFlags("--processor", "events", "--block-history", "200"),
	).Build(t, client, network)

	// Create the interchain environment with both chains and the relayer
	ic := interchaintest.NewInterchain().
		AddChain(chainA).
		AddChain(chainB).
		AddRelayer(r, "relayer")

	// Add an IBC link between the two chains
	ic = ic.AddLink(interchaintest.InterchainLink{
		Chain1:  chainA,
		Chain2:  chainB,
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

	// Wait for a few blocks to ensure chains are running properly
	require.NoError(t, testutil.WaitForBlocks(ctx, 5, chainA))

	// Create and fund test users on both chains
	fundAmount := math.NewInt(10_000_000)
	users := interchaintest.GetAndFundTestUsers(t, ctx, "default", fundAmount, chainA, chainB)
	userA := users[0]
	userB := users[1]

	// Verify initial balance of userA
	userAInitial, err := chainA.GetBalance(ctx, userA.FormattedAddress(), chainA.Config().Denom)
	require.NoError(t, err)
	require.True(t, userAInitial.Equal(fundAmount))

	// Get the IBC channel IDs for both chains
	aInfo, err := r.GetChannels(ctx, eRep, chainA.Config().ChainID)
	require.NoError(t, err)
	aChannelID, err := getTransferChannel(aInfo)
	require.NoError(t, err)

	bInfo, err := r.GetChannels(ctx, eRep, chainB.Config().ChainID)
	require.NoError(t, err)
	bChannelID, err := getTransferChannel(bInfo)
	require.NoError(t, err)

	// Prepare the IBC transfer from chainA to chainB
	amountToSend := math.NewInt(1_000_000)
	dstAddress := userB.FormattedAddress()
	transfer := ibc.WalletAmount{
		Address: dstAddress,
		Denom:   chainA.Config().Denom,
		Amount:  amountToSend,
	}

	// Execute the IBC transfer
	_, err = chainA.SendIBCTransfer(ctx, aChannelID, userA.KeyName(), transfer, ibc.TransferOptions{})
	require.NoError(t, err)

	// Relay the IBC packets between chains
	require.NoError(t, r.Flush(ctx, eRep, ibcPath, aChannelID))

	// Verify userA's balance has decreased by the transfer amount
	expectedBal := userAInitial.Sub(amountToSend)
	aNewBal, err := chainA.GetBalance(ctx, userA.FormattedAddress(), chainA.Config().Denom)
	require.NoError(t, err)
	require.True(t, aNewBal.Equal(expectedBal))

	// Calculate the IBC denom that will be received on chainB
	srcDenomTrace := transfertypes.ParseDenomTrace(transfertypes.GetPrefixedDenom("transfer", bChannelID, chainA.Config().Denom))
	dstIbcDenom := srcDenomTrace.IBCDenom()

	// Verify userB's balance has increased by the transfer amount with the correct IBC denom
	bNewBal, err := chainB.GetBalance(ctx, userB.FormattedAddress(), dstIbcDenom)
	require.NoError(t, err)
	require.True(t, bNewBal.Equal(amountToSend))
}
