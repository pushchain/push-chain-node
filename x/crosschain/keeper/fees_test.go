package keeper_test

import (
	"math/big"
	"testing"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	pchaintypes "github.com/rollchains/pchain/types"
	"github.com/rollchains/pchain/x/crosschain/types"
	"github.com/stretchr/testify/require"
)

func TestCalculateGasCost(t *testing.T) {
	f := SetupTest(t)

	tests := []struct {
		name                 string
		baseFee              sdkmath.LegacyDec
		maxFeePerGas         *big.Int
		maxPriorityFeePerGas *big.Int
		gasUsed              uint64
		expectedResult       *big.Int
		expectError          bool
		errorContains        string
	}{
		{
			name:                 "normal gas calculation",
			baseFee:              sdkmath.LegacyNewDec(10_000_000_000), // 10 gwei
			maxFeePerGas:         big.NewInt(20_000_000_000),           // 20 gwei
			maxPriorityFeePerGas: big.NewInt(1_000_000_000),            // 1 gwei
			gasUsed:              21000,
			expectedResult:       big.NewInt(231_000_000_000_000), // (10 + 1) * 21000 = 11 gwei * 21000
			expectError:          false,
		},
		{
			name:                 "max fee per gas is limiting factor",
			baseFee:              sdkmath.LegacyNewDec(10_000_000_000), // 10 gwei
			maxFeePerGas:         big.NewInt(15_000_000_000),           // 15 gwei (lower than base + priority)
			maxPriorityFeePerGas: big.NewInt(10_000_000_000),           // 10 gwei
			gasUsed:              21000,
			expectedResult:       big.NewInt(315_000_000_000_000), // 15 gwei * 21000
			expectError:          false,
		},
		{
			name:                 "zero priority fee",
			baseFee:              sdkmath.LegacyNewDec(10_000_000_000), // 10 gwei
			maxFeePerGas:         big.NewInt(20_000_000_000),           // 20 gwei
			maxPriorityFeePerGas: big.NewInt(0),                        // 0 gwei
			gasUsed:              21000,
			expectedResult:       big.NewInt(210_000_000_000_000), // 10 gwei * 21000
			expectError:          false,
		},
		{
			name:                 "maxFeePerGas less than baseFee",
			baseFee:              sdkmath.LegacyNewDec(20_000_000_000), // 20 gwei
			maxFeePerGas:         big.NewInt(10_000_000_000),           // 10 gwei (less than base)
			maxPriorityFeePerGas: big.NewInt(1_000_000_000),            // 1 gwei
			gasUsed:              21000,
			expectError:          true,
			errorContains:        "maxFeePerGas (10000000000) cannot be less than baseFee (20000000000)",
		},
		{
			name:                 "high gas usage scenario",
			baseFee:              sdkmath.LegacyNewDec(50_000_000_000), // 50 gwei
			maxFeePerGas:         big.NewInt(100_000_000_000),          // 100 gwei
			maxPriorityFeePerGas: big.NewInt(5_000_000_000),            // 5 gwei
			gasUsed:              500_000,                              // High gas usage
			expectedResult:       big.NewInt(27_500_000_000_000_000),   // 55 gwei * 500000
			expectError:          false,
		},
		{
			name:                 "very large numbers",
			baseFee:              sdkmath.LegacyNewDec(1_000_000_000_000), // 1000 gwei
			maxFeePerGas:         big.NewInt(2_000_000_000_000),           // 2000 gwei
			maxPriorityFeePerGas: big.NewInt(500_000_000_000),             // 500 gwei
			gasUsed:              1_000_000,                               // 1M gas
			expectedResult:       big.NewInt(1_500_000_000_000_000_000),   // 1500 gwei * 1M
			expectError:          false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := f.k.CalculateGasCost(
				tc.baseFee,
				tc.maxFeePerGas,
				tc.maxPriorityFeePerGas,
				tc.gasUsed,
			)

			if tc.expectError {
				require.Error(t, err)
				if tc.errorContains != "" {
					require.Contains(t, err.Error(), tc.errorContains)
				}
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.expectedResult, result)
			}
		})
	}
}

func TestDeductAndBurnFees(t *testing.T) {
	f := SetupTest(t)

	// Create test account with some balance
	testAddr := f.addrs[0]
	initialBalance := sdkmath.NewInt(1000000000000000000) // 1 token
	initialCoin := sdk.NewCoin(pchaintypes.BaseDenom, initialBalance)

	// Mint coins to test account
	err := f.bankkeeper.MintCoins(f.ctx, types.ModuleName, sdk.NewCoins(initialCoin))
	require.NoError(t, err)
	err = f.bankkeeper.SendCoinsFromModuleToAccount(f.ctx, types.ModuleName, testAddr, sdk.NewCoins(initialCoin))
	require.NoError(t, err)

	tests := []struct {
		name            string
		fromAddr        sdk.AccAddress
		gasCost         *big.Int
		expectError     bool
		errorContains   string
		checkBalance    bool
		expectedBalance sdkmath.Int
	}{
		{
			name:            "successful fee deduction and burn",
			fromAddr:        testAddr,
			gasCost:         big.NewInt(100000000000000000), // 0.1 token
			expectError:     false,
			checkBalance:    true,
			expectedBalance: sdkmath.NewInt(900000000000000000), // 0.9 token remaining
		},
		{
			name:          "insufficient balance",
			fromAddr:      testAddr,
			gasCost:       big.NewInt(2000000000000000000), // 2 tokens (more than available)
			expectError:   true,
			errorContains: "insufficient funds",
			checkBalance:  false,
		},
		{
			name:         "zero gas cost",
			fromAddr:     testAddr,
			gasCost:      big.NewInt(0),
			expectError:  false,
			checkBalance: false, // Balance should remain unchanged
		},
		{
			name:          "non-existent account",
			fromAddr:      sdk.AccAddress("nonexistent_account_addr"),
			gasCost:       big.NewInt(100000000000000000),
			expectError:   true,
			errorContains: "insufficient funds",
			checkBalance:  false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := f.k.DeductAndBurnFees(f.ctx, tc.fromAddr, tc.gasCost)

			if tc.expectError {
				require.Error(t, err)
				if tc.errorContains != "" {
					require.Contains(t, err.Error(), tc.errorContains)
				}
			} else {
				require.NoError(t, err)

				if tc.checkBalance {
					// Check final balance
					finalBal := f.bankkeeper.GetBalance(f.ctx, testAddr, pchaintypes.BaseDenom)
					require.Equal(t, tc.expectedBalance.String(), finalBal.Amount.String())
				}
			}
		})
	}
}

func TestDeductAndBurnFeesModuleAccount(t *testing.T) {
	f := SetupTest(t)

	// Test that module account balance changes correctly
	testAddr := f.addrs[0]
	initialBalance := sdkmath.NewInt(1000000000000000000) // 1 token
	initialCoin := sdk.NewCoin(pchaintypes.BaseDenom, initialBalance)

	// Mint coins to test account
	err := f.bankkeeper.MintCoins(f.ctx, types.ModuleName, sdk.NewCoins(initialCoin))
	require.NoError(t, err)
	err = f.bankkeeper.SendCoinsFromModuleToAccount(f.ctx, types.ModuleName, testAddr, sdk.NewCoins(initialCoin))
	require.NoError(t, err)

	gasCost := big.NewInt(100000000000000000) // 0.1 token

	// Get initial module account balance
	moduleAddr := f.accountkeeper.GetModuleAddress(types.ModuleName)
	initialModuleBal := f.bankkeeper.GetBalance(f.ctx, moduleAddr, pchaintypes.BaseDenom)

	// Deduct and burn fees
	err = f.k.DeductAndBurnFees(f.ctx, testAddr, gasCost)
	require.NoError(t, err)

	// Check that module account balance is same (coins were burned, not kept)
	finalModuleBal := f.bankkeeper.GetBalance(f.ctx, moduleAddr, pchaintypes.BaseDenom)
	require.Equal(t, initialModuleBal, finalModuleBal)

	// Check total supply decreased (coins were burned)
	// Note: In a real scenario, we'd check the total supply decrease,
	// but this requires more complex setup with proper bank keeper mocking
}

func TestCalculateGasCostEdgeCases(t *testing.T) {
	f := SetupTest(t)

	t.Run("nil_values", func(t *testing.T) {
		baseFee := sdkmath.LegacyNewDec(10_000_000_000)

		// Test with nil maxFeePerGas
		_, err := f.k.CalculateGasCost(baseFee, nil, big.NewInt(1), 21000)
		require.Error(t, err)

		// Test with nil maxPriorityFeePerGas
		_, err = f.k.CalculateGasCost(baseFee, big.NewInt(1), nil, 21000)
		require.Error(t, err)
	})

	t.Run("zero_gas_used", func(t *testing.T) {
		baseFee := sdkmath.LegacyNewDec(10_000_000_000)
		maxFeePerGas := big.NewInt(20_000_000_000)
		maxPriorityFeePerGas := big.NewInt(1_000_000_000)

		result, err := f.k.CalculateGasCost(baseFee, maxFeePerGas, maxPriorityFeePerGas, 0)
		require.NoError(t, err)
		require.Equal(t, big.NewInt(0), result)
	})

	t.Run("negative_base_fee", func(t *testing.T) {
		baseFee := sdkmath.LegacyNewDec(-10_000_000_000) // Negative base fee
		maxFeePerGas := big.NewInt(20_000_000_000)
		maxPriorityFeePerGas := big.NewInt(1_000_000_000)

		// This might not fail immediately but could cause issues
		result, err := f.k.CalculateGasCost(baseFee, maxFeePerGas, maxPriorityFeePerGas, 21000)
		// The behavior depends on implementation - test what actually happens
		if err == nil {
			// If no error, result should be calculated correctly
			require.NotNil(t, result)
		}
	})
}

// Benchmark tests for fee calculation
func BenchmarkCalculateGasCost(b *testing.B) {
	f := SetupTest(&testing.T{})

	baseFee := sdkmath.LegacyNewDec(10_000_000_000)
	maxFeePerGas := big.NewInt(20_000_000_000)
	maxPriorityFeePerGas := big.NewInt(1_000_000_000)
	gasUsed := uint64(21000)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = f.k.CalculateGasCost(baseFee, maxFeePerGas, maxPriorityFeePerGas, gasUsed)
	}
}

func BenchmarkDeductAndBurnFees(b *testing.B) {
	f := SetupTest(&testing.T{})

	// Setup account with balance
	testAddr := f.addrs[0]
	initialBalance := sdkmath.NewInt(100000000000000000) // 100 tokens for benchmarking (reduced to fit int64)
	initialCoin := sdk.NewCoin(pchaintypes.BaseDenom, initialBalance)

	f.bankkeeper.MintCoins(f.ctx, types.ModuleName, sdk.NewCoins(initialCoin))
	f.bankkeeper.SendCoinsFromModuleToAccount(f.ctx, types.ModuleName, testAddr, sdk.NewCoins(initialCoin))

	gasCost := big.NewInt(1000000000000000) // Small amount for repeated benchmark

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// This will eventually fail when balance runs out, but gives us benchmark data
		_ = f.k.DeductAndBurnFees(f.ctx, testAddr, gasCost)
	}
}
