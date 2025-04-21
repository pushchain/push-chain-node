package e2e

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/strangelove-ventures/interchaintest/v8"
	"github.com/strangelove-ventures/interchaintest/v8/chain/cosmos"
	"github.com/strangelove-ventures/interchaintest/v8/ibc"
	"github.com/strangelove-ventures/interchaintest/v8/testreporter"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// GetCountResponse represents the response structure from the CosmWasm contract's get_count query
type GetCountResponse struct {
	// {"data":{"count":0}}
	Data *GetCountObj `json:"data"`
}

// GetCountObj holds the actual count value from the contract's state
type GetCountObj struct {
	Count int64 `json:"count"`
}

// TestCosmWasmIntegration verifies that CosmWasm smart contracts can be deployed and executed on the chain.
// This test ensures that:
// 1. A chain with CosmWasm support can be properly initialized
// 2. A CosmWasm contract can be stored on the chain
// 3. The contract can be instantiated with initial state
// 4. Contract execution (state changes) works correctly
// 5. Contract queries return the expected results
func TestCosmWasmIntegration(t *testing.T) {
	t.Parallel()
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

	// Create and fund a test user
	users := interchaintest.GetAndFundTestUsers(t, ctx, t.Name(), GenesisFundsAmount, chain)
	user := users[0]

	// Execute the standard CosmWasm test flow
	StdExecute(t, ctx, chain, user)
}

// StdExecute performs a standard flow of CosmWasm operations:
// 1. Uploads a contract to the chain
// 2. Instantiates the contract with initial state
// 3. Executes a transaction on the contract to increment the counter
// 4. Queries the contract to verify the state change
func StdExecute(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, user ibc.Wallet) (contractAddr string) {
	// Upload and instantiate the contract with initial count of 0
	_, contractAddr = SetupContract(t, ctx, chain, user.KeyName(), "contracts/cw_template.wasm", `{"count":0}`)

	// Execute the increment operation on the contract
	chain.ExecuteContract(ctx, user.KeyName(), contractAddr, `{"increment":{}}`, "--fees", "10000"+chain.Config().Denom)

	// Query the contract to verify the count was incremented
	var res GetCountResponse
	err := SmartQueryString(t, ctx, chain, contractAddr, `{"get_count":{}}`, &res)
	require.NoError(t, err)

	// Verify the count is now 1 after the increment operation
	require.Equal(t, int64(1), res.Data.Count)

	return contractAddr
}

// SmartQueryString performs a query on a CosmWasm contract and unmarshals the result into the provided response object
func SmartQueryString(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, contractAddr, queryMsg string, res interface{}) error {
	// Convert the query string to a JSON map for the chain's QueryContract method
	var jsonMap map[string]interface{}
	if err := json.Unmarshal([]byte(queryMsg), &jsonMap); err != nil {
		t.Fatal(err)
	}
	// Execute the query and unmarshal the result into the provided response object
	err := chain.QueryContract(ctx, contractAddr, jsonMap, &res)
	return err
}

// SetupContract uploads a CosmWasm contract to the chain and instantiates it
// Returns the code ID and contract address
func SetupContract(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, keyname string, fileLoc string, message string, extraFlags ...string) (codeId, contract string) {
	// Store the contract on the chain
	codeId, err := chain.StoreContract(ctx, keyname, fileLoc)
	if err != nil {
		t.Fatal(err)
	}

	// Determine if we need to add the --no-admin flag
	needsNoAdminFlag := true
	for _, flag := range extraFlags {
		if flag == "--admin" {
			needsNoAdminFlag = false
		}
	}

	// Instantiate the contract with the provided message
	contractAddr, err := chain.InstantiateContract(ctx, keyname, codeId, message, needsNoAdminFlag, extraFlags...)
	if err != nil {
		t.Fatal(err)
	}

	return codeId, contractAddr
}
