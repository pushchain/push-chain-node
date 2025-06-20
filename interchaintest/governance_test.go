package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"cosmossdk.io/math"

	"github.com/strangelove-ventures/interchaintest/v8"
	"github.com/strangelove-ventures/interchaintest/v8/chain/cosmos"
	"github.com/strangelove-ventures/interchaintest/v8/testreporter"
	"github.com/strangelove-ventures/interchaintest/v8/testutil"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// TestGovernanceProposal verifies that users can submit governance proposals on the Rollchain blockchain.
func TestGovernanceProposal(t *testing.T) {
	ctx := context.Background()
	rep := testreporter.NewNopReporter()
	eRep := rep.RelayerExecReporter(t)

	// Set up Docker environment for the test
	client, network := interchaintest.DockerSetup(t)

	// Modify the genesis to have very short voting periods for testing
	modifiedGenesis := []cosmos.GenesisKV{
		// Set deposit params for proposal submission
		cosmos.NewGenesisKV("app_state.gov.params.min_deposit.0.denom", Denom),
		cosmos.NewGenesisKV("app_state.gov.params.min_deposit.0.amount", "1"),
	}

	// Create a chain config with the modified genesis
	chainConfig := DefaultChainConfig
	chainConfig.ModifyGenesis = cosmos.ModifyGenesis(modifiedGenesis)
	chainConfig.GasPrices = "586181640.625000907913440340" + Denom // This was the proposed gas price for the governance proposal creation

	// Create a chain spec with the modified chain config
	chainSpec := DefaultChainSpec
	chainSpec.ChainConfig = chainConfig

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
		SkipPathCreation: false,
	}))

	// Clean up resources when the test completes
	t.Cleanup(func() {
		_ = ic.Close()
	})

	// Create and fund a test user (proposer) with 10 million tokens
	proposerFunds := math.NewInt(10_000_000)
	proposers := interchaintest.GetAndFundTestUsers(t, ctx, "proposer", proposerFunds, chain)
	proposer := proposers[0]

	t.Run("create_and_process_a_text_proposal", func(t *testing.T) {
		// Create a text proposal
		proposalTitle := "Test Proposal"
		proposalSummary := "This is a test proposal for e2e testing"
		proposalMetadata := "ipfs://CID"
		depositAmount := fmt.Sprintf("1%s", chain.Config().Denom)

		// Create a JSON file for the proposal
		textProposalJSON := fmt.Sprintf(`{
			"messages": [
				{
					"@type": "/cosmos.gov.v1.MsgExecLegacyContent",
					"authority": "push10d07y265gmmuvt4z0w9aw880jnsr700j3hneqr",
					"content": {
						"@type": "/cosmos.gov.v1beta1.TextProposal",
						"title": "%s",
						"description": "%s"
					}
				}
			],
			"metadata": "%s",
			"deposit": "%s",
			"title": "%s",
			"summary": "%s"
		}`, proposalTitle, proposalSummary, proposalMetadata, depositAmount, proposalTitle, proposalSummary)

		// Create a text proposal JSON file
		cmd := []string{
			"sh", "-c", fmt.Sprintf(`echo '%s' > %s/proposal.json`, textProposalJSON, chain.HomeDir()),
		}
		_, _, err := chain.Exec(ctx, cmd, nil)
		require.NoError(t, err, "Failed to create proposal.json")

		// Submit the proposal
		cmd = TxCommandBuilder(ctx, chain, []string{
			"tx", "gov", "submit-proposal",
			fmt.Sprintf("%s/proposal.json", chain.HomeDir()),
		}, proposer.KeyName(), "--gas-prices", "586181640.625000907913440340upc")

		out, _, err := chain.Exec(ctx, cmd, nil)
		require.NoError(t, err, "Failed to submit governance proposal")

		// Wait for blocks to ensure the transaction is processed
		err = testutil.WaitForBlocks(ctx, 2, chain)
		require.NoError(t, err, "Failed to wait for blocks")

		// Parse the transaction response to get the hash
		var txResponse struct {
			TxHash string `json:"txhash"`
		}
		err = json.Unmarshal(out, &txResponse)
		require.NoError(t, err, "Failed to parse transaction response")
		require.NotEmpty(t, txResponse.TxHash, "Transaction hash is empty")

		t.Logf("Proposal submitted successfully with transaction hash: %s", txResponse.TxHash)
	})
}
