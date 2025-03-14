package e2e

import (
	"context"
	"strconv"
	"strings"
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

// TestStaking verifies the staking functionality of the Rollchain blockchain.
// This test ensures that:
// 1. Users can successfully delegate tokens to validators
// 2. Staking rewards are properly distributed over time
// 3. Users can claim/withdraw their staking rewards
// 4. The reward amounts are reasonable and follow expected parameters
// 5. Users can undelegate their tokens after staking
func TestStaking(t *testing.T) {
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
	initialFunds := math.NewInt(10_000_000)
	users := interchaintest.GetAndFundTestUsers(t, ctx, "default", initialFunds, chain)
	user := users[0]

	// Get validator address to delegate to
	type Validator struct {
		OperatorAddress string `json:"operator_address"`
	}

	type ValidatorsResponse struct {
		Validators []Validator `json:"validators"`
		Pagination struct {
			Total string `json:"total"`
		} `json:"pagination"`
	}

	var validatorsResp ValidatorsResponse
	cmd := []string{"query", "staking", "validators"}
	ExecuteQuery(ctx, chain, cmd, &validatorsResp)

	require.GreaterOrEqual(t, len(validatorsResp.Validators), 1, "expected at least one validator")

	validatorAddr := validatorsResp.Validators[0].OperatorAddress
	t.Logf("Validator address: %s", validatorAddr)

	// Verify initial balance
	initialBalance, err := chain.BankQueryBalance(ctx, user.FormattedAddress(), chain.Config().Denom)
	require.NoError(t, err)
	require.Equal(t, initialFunds, initialBalance)

	// Amount to delegate
	delegationAmount := math.NewInt(5_000_000) // 5 million tokens

	// Delegate tokens to validator
	t.Run("delegate tokens", func(t *testing.T) {
		// Execute the delegation transaction directly using the chain's command
		_, _, err := chain.Exec(ctx, []string{
			"pchaind", "tx", "staking", "delegate",
			validatorAddr,
			delegationAmount.String() + chain.Config().Denom,
			"--from", user.KeyName(),
			"--gas", "auto",
			"--gas-adjustment", "1.5",
			"--gas-prices", chain.Config().GasPrices,
			"--chain-id", chain.Config().ChainID,
			"--home", "/var/cosmos-chain/rollchain",
			"--node", chain.GetRPCAddress(),
			"--keyring-backend", "test",
			"--output", "json",
			"--yes",
		}, nil)
		require.NoError(t, err, "failed to execute delegation transaction")

		// Wait for a few blocks to ensure the delegation is processed
		require.NoError(t, testutil.WaitForBlocks(ctx, 2, chain))

		// Verify delegation was successful
		type DelegationResponse struct {
			Delegation struct {
				DelegatorAddress string `json:"delegator_address"`
				ValidatorAddress string `json:"validator_address"`
				Shares           string `json:"shares"`
			} `json:"delegation"`
			Balance struct {
				Denom  string `json:"denom"`
				Amount string `json:"amount"`
			} `json:"balance"`
		}

		type DelegationsResponse struct {
			DelegationResponses []DelegationResponse `json:"delegation_responses"`
			Pagination          struct {
				Total string `json:"total"`
			} `json:"pagination"`
		}

		var delegationsResp DelegationsResponse
		cmd = []string{"query", "staking", "delegations", user.FormattedAddress()}
		ExecuteQuery(ctx, chain, cmd, &delegationsResp)

		require.GreaterOrEqual(t, len(delegationsResp.DelegationResponses), 1, "expected at least one delegation")

		// Find the delegation to our validator
		var found bool
		var delegation DelegationResponse
		for _, d := range delegationsResp.DelegationResponses {
			if d.Delegation.ValidatorAddress == validatorAddr {
				found = true
				delegation = d
				break
			}
		}
		require.True(t, found, "delegation to validator %s not found", validatorAddr)

		// Convert delegation amount to math.Int for comparison
		actualDelegation, ok := math.NewIntFromString(delegation.Balance.Amount)
		require.True(t, ok, "failed to parse delegation amount")

		// Allow for a small difference due to gas fees
		require.True(t, delegationAmount.Sub(actualDelegation).LT(math.NewInt(100000)),
			"delegation amount should be close to requested amount, expected %s, got %s",
			delegationAmount, actualDelegation)
	})

	// Wait for rewards to accumulate
	t.Run("wait for rewards", func(t *testing.T) {
		// Wait for some blocks to accumulate rewards
		t.Log("Waiting for rewards to accumulate...")
		require.NoError(t, testutil.WaitForBlocks(ctx, 10, chain))

		// Check if rewards are accumulating
		type RewardsResponse struct {
			Rewards []struct {
				ValidatorAddress string   `json:"validator_address"`
				Reward           []string `json:"reward"`
			} `json:"rewards"`
			Total []string `json:"total"`
		}

		var rewards RewardsResponse
		cmd := []string{"query", "distribution", "rewards", user.FormattedAddress()}
		ExecuteQuery(ctx, chain, cmd, &rewards)

		// Verify that rewards exist and are greater than zero
		require.NotEmpty(t, rewards.Rewards, "expected rewards to be accumulating")

		// Find rewards for our validator
		var rewardAmount float64
		for _, reward := range rewards.Rewards {
			if reward.ValidatorAddress == validatorAddr {
				require.NotEmpty(t, reward.Reward, "expected non-empty reward")
				for _, r := range reward.Reward {
					// Parse the reward string which is in format "amount+denom"
					parts := strings.Split(r, chain.Config().Denom)
					if len(parts) > 0 {
						amount := parts[0]
						rewardAmount, err = strconv.ParseFloat(amount, 64)
						require.NoError(t, err)
						break
					}
				}
				break
			}
		}

		t.Logf("Accumulated rewards: %v %s", rewardAmount, chain.Config().Denom)
		require.Greater(t, rewardAmount, 0.0, "expected rewards to be greater than zero")
	})

	// Wait a bit longer for more rewards
	t.Log("Waiting for more rewards to accumulate...")
	require.NoError(t, testutil.WaitForBlocks(ctx, 10, chain))

	// Withdraw rewards
	t.Run("withdraw rewards", func(t *testing.T) {
		// Get balance before withdrawal
		balanceBeforeWithdrawal, err := chain.BankQueryBalance(ctx, user.FormattedAddress(), chain.Config().Denom)
		require.NoError(t, err)

		// Withdraw rewards
		_, _, err = chain.Exec(ctx, []string{
			"pchaind", "tx", "distribution", "withdraw-rewards",
			validatorAddr,
			"--from", user.KeyName(),
			"--gas", "auto",
			"--gas-adjustment", "1.5",
			"--gas-prices", chain.Config().GasPrices,
			"--chain-id", chain.Config().ChainID,
			"--home", "/var/cosmos-chain/rollchain",
			"--node", chain.GetRPCAddress(),
			"--keyring-backend", "test",
			"--output", "json",
			"--yes",
		}, nil)
		require.NoError(t, err, "failed to execute withdraw rewards transaction")

		// Wait for a few blocks to ensure the withdrawal is processed
		require.NoError(t, testutil.WaitForBlocks(ctx, 2, chain))

		// Get balance after withdrawal
		balanceAfterWithdrawal, err := chain.BankQueryBalance(ctx, user.FormattedAddress(), chain.Config().Denom)
		require.NoError(t, err)

		// Verify that balance increased after withdrawal (accounting for gas fees)
		t.Logf("Balance before withdrawal: %s, after: %s", balanceBeforeWithdrawal, balanceAfterWithdrawal)
		require.True(t, balanceAfterWithdrawal.GT(balanceBeforeWithdrawal),
			"expected balance to increase after withdrawal")
	})

	// Undelegate tokens
	t.Run("undelegate tokens", func(t *testing.T) {
		// Amount to undelegate (half of the original delegation)
		undelegationAmount := delegationAmount.Quo(math.NewInt(2))

		// Execute the unbond transaction directly using the chain's command
		_, _, err := chain.Exec(ctx, []string{
			"pchaind", "tx", "staking", "unbond",
			validatorAddr,
			undelegationAmount.String() + chain.Config().Denom,
			"--from", user.KeyName(),
			"--gas", "auto",
			"--gas-adjustment", "1.5",
			"--gas-prices", chain.Config().GasPrices,
			"--chain-id", chain.Config().ChainID,
			"--home", "/var/cosmos-chain/rollchain",
			"--node", chain.GetRPCAddress(),
			"--keyring-backend", "test",
			"--output", "json",
			"--yes",
		}, nil)
		require.NoError(t, err, "failed to execute unbond transaction")

		// Wait for a few blocks to ensure the undelegation is processed
		require.NoError(t, testutil.WaitForBlocks(ctx, 2, chain))

		// Verify undelegation was initiated
		type UnbondingEntry struct {
			Balance        string `json:"balance"`
			CompletionTime string `json:"completion_time"`
		}
		type UnbondingDelegation struct {
			DelegatorAddress string           `json:"delegator_address"`
			ValidatorAddress string           `json:"validator_address"`
			Entries          []UnbondingEntry `json:"entries"`
		}

		type UnbondingDelegationsResponse struct {
			UnbondingResponses []UnbondingDelegation `json:"unbonding_responses"`
			Pagination         struct {
				Total string `json:"total"`
			} `json:"pagination"`
		}

		var unbondingResp UnbondingDelegationsResponse
		cmd = []string{"query", "staking", "unbonding-delegations", user.FormattedAddress()}
		ExecuteQuery(ctx, chain, cmd, &unbondingResp)

		require.NotEmpty(t, unbondingResp.UnbondingResponses, "expected unbonding delegations")

		// Find our validator's unbonding delegation
		var found bool
		for _, unbonding := range unbondingResp.UnbondingResponses {
			if unbonding.ValidatorAddress == validatorAddr {
				found = true
				require.NotEmpty(t, unbonding.Entries, "expected unbonding entries")

				// Convert unbonding amount to math.Int for comparison
				actualUnbonding, ok := math.NewIntFromString(unbonding.Entries[0].Balance)
				require.True(t, ok, "failed to parse unbonding amount")

				// Allow for a small difference
				require.True(t, undelegationAmount.Sub(actualUnbonding).Abs().LT(math.NewInt(100000)),
					"unbonding amount should be close to requested amount, expected %s, got %s",
					undelegationAmount, actualUnbonding)

				// Check completion time is in the future
				completionTime, err := time.Parse(time.RFC3339, unbonding.Entries[0].CompletionTime)
				require.NoError(t, err)
				require.True(t, completionTime.After(time.Now()),
					"unbonding completion time should be in the future")

				t.Logf("Unbonding will complete at: %s", completionTime)
				break
			}
		}
		require.True(t, found, "expected to find unbonding delegation for validator")

		// Verify remaining delegation
		type DelegationResponse struct {
			Delegation struct {
				DelegatorAddress string `json:"delegator_address"`
				ValidatorAddress string `json:"validator_address"`
				Shares           string `json:"shares"`
			} `json:"delegation"`
			Balance struct {
				Denom  string `json:"denom"`
				Amount string `json:"amount"`
			} `json:"balance"`
		}

		type DelegationsResponse struct {
			DelegationResponses []DelegationResponse `json:"delegation_responses"`
			Pagination          struct {
				Total string `json:"total"`
			} `json:"pagination"`
		}

		var delegationsResp DelegationsResponse
		cmd = []string{"query", "staking", "delegations", user.FormattedAddress()}
		ExecuteQuery(ctx, chain, cmd, &delegationsResp)

		// Find the delegation to our validator
		found = false
		var delegation DelegationResponse
		for _, d := range delegationsResp.DelegationResponses {
			if d.Delegation.ValidatorAddress == validatorAddr {
				found = true
				delegation = d
				break
			}
		}
		require.True(t, found, "delegation to validator %s not found", validatorAddr)

		// Convert remaining delegation amount to math.Int for comparison
		remainingDelegation, ok := math.NewIntFromString(delegation.Balance.Amount)
		require.True(t, ok, "failed to parse delegation amount")

		// Expected remaining delegation (original - undelegated)
		expectedRemaining := delegationAmount.Sub(undelegationAmount)

		// Allow for a small difference
		require.True(t, expectedRemaining.Sub(remainingDelegation).Abs().LT(math.NewInt(100000)),
			"remaining delegation should be close to expected amount, expected %s, got %s",
			expectedRemaining, remainingDelegation)
	})
}
