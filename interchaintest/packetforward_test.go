package e2e

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"cosmossdk.io/math"
	"github.com/strangelove-ventures/interchaintest/v8"
	"github.com/strangelove-ventures/interchaintest/v8/chain/cosmos"
	"github.com/strangelove-ventures/interchaintest/v8/ibc"
	interchaintestrelayer "github.com/strangelove-ventures/interchaintest/v8/relayer"
	"github.com/strangelove-ventures/interchaintest/v8/testreporter"
	"github.com/strangelove-ventures/interchaintest/v8/testutil"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	transfertypes "github.com/cosmos/ibc-go/v8/modules/apps/transfer/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// PacketMetadata defines the structure for packet forwarding metadata
type PacketMetadata struct {
	Forward *ForwardMetadata `json:"forward"`
}

// ForwardMetadata contains the details needed for packet forwarding
type ForwardMetadata struct {
	Receiver       string        `json:"receiver"`
	Port           string        `json:"port"`
	Channel        string        `json:"channel"`
	Timeout        time.Duration `json:"timeout"`
	Retries        *uint8        `json:"retries,omitempty"`
	Next           *string       `json:"next,omitempty"`
	RefundSequence *uint64       `json:"refund_sequence,omitempty"`
}

// TestPacketForwardMiddleware verifies the functionality of the packet forward middleware.
// This test ensures that:
// 1. Three separate chains can be initialized and connected via IBC
// 2. IBC channels can be established between Chain A and Chain B, and between Chain B and Chain C
// 3. A token transfer can be initiated from Chain A to Chain B with forwarding metadata
// 4. The packet forward middleware correctly forwards the tokens from Chain B to Chain C
// 5. The final recipient on Chain C receives the correct amount of tokens
// 6. The escrow accounts on both chains hold the correct amounts
func TestPacketForwardMiddleware(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}

	ctx := context.Background()
	rep := testreporter.NewNopReporter()
	eRep := rep.RelayerExecReporter(t)
	// Set up Docker environment for the test
	client, network := interchaintest.DockerSetup(t)

	var (
		chainID_A, chainID_B, chainID_C = "chain-a", "chain-b", "chain-c"
		chainA, chainB, chainC          *cosmos.CosmosChain
	)

	// Define the base configuration for all chains
	baseCfg := DefaultChainConfig

	// Configure Chain A with its specific chain ID
	baseCfg.ChainID = chainID_A
	configA := baseCfg

	// Configure Chain B with its specific chain ID
	baseCfg.ChainID = chainID_B
	configB := baseCfg

	// Configure Chain C with its specific chain ID
	baseCfg.ChainID = chainID_C
	configC := baseCfg

	// Set the number of validators and full nodes for each chain
	numVals := 1
	numFullNodes := 0

	// Create a chain factory with three individual networks
	cf := interchaintest.NewBuiltinChainFactory(zaptest.NewLogger(t), []*interchaintest.ChainSpec{
		{
			Name:          configA.Name,
			ChainConfig:   configA,
			NumValidators: &numVals,
			NumFullNodes:  &numFullNodes,
		},
		{
			Name:          configA.Name,
			ChainConfig:   configB,
			NumValidators: &numVals,
			NumFullNodes:  &numFullNodes,
		},
		{
			Name:          configA.Name,
			ChainConfig:   configC,
			NumValidators: &numVals,
			NumFullNodes:  &numFullNodes,
		},
	})

	// Initialize the chains from the factory
	chains, err := cf.Chains(t.Name())
	require.NoError(t, err)

	// Get references to all three chains
	chainA, chainB, chainC = chains[0].(*cosmos.CosmosChain), chains[1].(*cosmos.CosmosChain), chains[2].(*cosmos.CosmosChain)

	// Set up the relayer for IBC communication between chains
	r := interchaintest.NewBuiltinRelayerFactory(
		ibc.CosmosRly,
		zaptest.NewLogger(t),
		interchaintestrelayer.CustomDockerImage(RelayerRepo, RelayerVersion, "100:1000"),
		interchaintestrelayer.StartupFlags("--processor", "events", "--block-history", "100"),
	).Build(t, client, network)

	// Define path names for the IBC connections
	const pathAB = "ab"
	const pathBC = "bc"

	// Create the interchain environment with all three chains and the relayer
	ic := interchaintest.NewInterchain().
		AddChain(chainA).
		AddChain(chainB).
		AddChain(chainC).
		AddRelayer(r, "relayer").
		// Add IBC link between Chain A and Chain B
		AddLink(interchaintest.InterchainLink{
			Chain1:  chainA,
			Chain2:  chainB,
			Relayer: r,
			Path:    pathAB,
		}).
		// Add IBC link between Chain B and Chain C
		AddLink(interchaintest.InterchainLink{
			Chain1:  chainB,
			Chain2:  chainC,
			Relayer: r,
			Path:    pathBC,
		})

	// Build the interchain environment
	require.NoError(t, ic.Build(ctx, eRep, interchaintest.InterchainBuildOptions{
		TestName:          t.Name(),
		Client:            client,
		NetworkID:         network,
		BlockDatabaseFile: interchaintest.DefaultBlockDatabaseFilepath(),

		SkipPathCreation: false,
	}))
	// Clean up resources when the test completes
	t.Cleanup(func() {
		_ = ic.Close()
	})

	// Create and fund test users on all three chains
	users := interchaintest.GetAndFundTestUsers(t, ctx, t.Name(), GenesisFundsAmount, chainA, chainB, chainC)

	// Get the IBC channel between Chain A and Chain B
	abChan, err := ibc.GetTransferChannel(ctx, r, eRep, chainID_A, chainID_B)
	require.NoError(t, err)

	// Get the counterparty channel on Chain B
	baChan := abChan.Counterparty

	// Get the IBC channel between Chain C and Chain B
	cbChan, err := ibc.GetTransferChannel(ctx, r, eRep, chainID_C, chainID_B)
	require.NoError(t, err)

	// Get the counterparty channel on Chain B
	bcChan := cbChan.Counterparty

	// Start the relayer on both paths to facilitate IBC communication
	err = r.StartRelayer(ctx, eRep, pathAB, pathBC)
	require.NoError(t, err)

	// Ensure the relayer is stopped after the test
	t.Cleanup(
		func() {
			err := r.StopRelayer(ctx, eRep)
			if err != nil {
				t.Logf("an error occurred while stopping the relayer: %s", err)
			}
		},
	)

	// Get references to the test users on each chain
	userA, userB, userC := users[0], users[1], users[2]

	// Define the amount to transfer
	var transferAmount math.Int = math.NewInt(100_000)

	// Calculate the IBC denominations for tracing tokens across chains
	// First hop: Chain A -> Chain B
	firstHopDenom := transfertypes.GetPrefixedDenom(baChan.PortID, baChan.ChannelID, chainA.Config().Denom)
	// Second hop: Chain B -> Chain C
	secondHopDenom := transfertypes.GetPrefixedDenom(cbChan.PortID, cbChan.ChannelID, firstHopDenom)

	// Parse the denomination traces
	firstHopDenomTrace := transfertypes.ParseDenomTrace(firstHopDenom)
	secondHopDenomTrace := transfertypes.ParseDenomTrace(secondHopDenom)

	// Convert to IBC denominations for balance checking
	firstHopIBCDenom := firstHopDenomTrace.IBCDenom()
	secondHopIBCDenom := secondHopDenomTrace.IBCDenom()

	// Calculate the escrow account addresses on both chains
	firstHopEscrowAccount := sdk.MustBech32ifyAddressBytes(chainA.Config().Bech32Prefix, transfertypes.GetEscrowAddress(abChan.PortID, abChan.ChannelID))
	secondHopEscrowAccount := sdk.MustBech32ifyAddressBytes(chainB.Config().Bech32Prefix, transfertypes.GetEscrowAddress(bcChan.PortID, bcChan.ChannelID))

	t.Run("multi-hop a->b->c", func(t *testing.T) {
		// Test sending tokens from Chain A through Chain B to Chain C

		// Prepare the transfer from Chain A to Chain B
		transfer := ibc.WalletAmount{
			Address: userB.FormattedAddress(),
			Denom:   chainA.Config().Denom,
			Amount:  transferAmount,
		}

		// Create the forwarding metadata to instruct Chain B to forward to Chain C
		firstHopMetadata := &PacketMetadata{
			Forward: &ForwardMetadata{
				Receiver: userC.FormattedAddress(),
				Channel:  bcChan.ChannelID,
				Port:     bcChan.PortID,
			},
		}

		// Convert the metadata to JSON for the transfer memo
		memo, err := json.Marshal(firstHopMetadata)
		require.NoError(t, err)

		// Get the current height of Chain A
		chainAHeight, err := chainA.Height(ctx)
		require.NoError(t, err)

		// Execute the IBC transfer from Chain A to Chain B with forwarding metadata
		transferTx, err := chainA.SendIBCTransfer(ctx, abChan.ChannelID, userA.KeyName(), transfer, ibc.TransferOptions{Memo: string(memo)})
		require.NoError(t, err)

		// Wait for the acknowledgement of the transfer
		_, err = testutil.PollForAck(ctx, chainA, chainAHeight, chainAHeight+30, transferTx.Packet)
		require.NoError(t, err)

		// Wait for a block to ensure all state changes are processed
		err = testutil.WaitForBlocks(ctx, 1, chainA)
		require.NoError(t, err)

		// Check the balance of userA on Chain A (should be reduced by the transfer amount)
		chainABalance, err := chainA.GetBalance(ctx, userA.FormattedAddress(), chainA.Config().Denom)
		require.NoError(t, err)

		// Check the balance of userB on Chain B (should be 0 as tokens were forwarded)
		chainBBalance, err := chainB.GetBalance(ctx, userB.FormattedAddress(), firstHopIBCDenom)
		require.NoError(t, err)

		// Check the balance of userC on Chain C (should have received the transferred amount)
		chainCBalance, err := chainC.GetBalance(ctx, userC.FormattedAddress(), secondHopIBCDenom)
		require.NoError(t, err)

		// Verify the balances on all chains
		require.Equal(t, GenesisFundsAmount.Sub(transferAmount).Int64(), chainABalance.Int64())
		require.Equal(t, int64(0), chainBBalance.Int64())
		require.Equal(t, int64(100000), chainCBalance.Int64())

		// Check the escrow account balance on Chain A
		firstHopEscrowBalance, err := chainA.GetBalance(ctx, firstHopEscrowAccount, chainA.Config().Denom)
		require.NoError(t, err)

		// Check the escrow account balance on Chain B
		secondHopEscrowBalance, err := chainB.GetBalance(ctx, secondHopEscrowAccount, firstHopIBCDenom)
		require.NoError(t, err)

		// Verify the escrow account balances
		require.Equal(t, transferAmount.Int64(), firstHopEscrowBalance.Int64())
		require.Equal(t, transferAmount.Int64(), secondHopEscrowBalance.Int64())
	})
}
