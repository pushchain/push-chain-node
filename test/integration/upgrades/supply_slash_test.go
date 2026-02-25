package integrationtest

import (
	"testing"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	"github.com/stretchr/testify/require"

	supplyslash "github.com/pushchain/push-chain-node/app/upgrades/supply-slash"
	pchaintypes "github.com/pushchain/push-chain-node/types"
	utils "github.com/pushchain/push-chain-node/test/utils"
	uvalidatortypes "github.com/pushchain/push-chain-node/x/uvalidator/types"
)

// onePCInUPC is 10^18 — the multiplier from whole PC tokens to raw upc base units.
var onePCInUPC = sdkmath.NewInt(1_000_000_000_000_000_000)

func TestFlushUValidatorToDistribution_WithBalance(t *testing.T) {
	chainApp := utils.SetupApp(t)
	ctx := chainApp.BaseApp.NewContext(true)

	// Fund the uvalidator module with 500 PC via the mint module
	flushAmt := sdkmath.NewInt(500).Mul(onePCInUPC)
	flushCoins := sdk.NewCoins(sdk.NewCoin(pchaintypes.BaseDenom, flushAmt))
	require.NoError(t, chainApp.BankKeeper.MintCoins(ctx, utils.MintModule, flushCoins))
	require.NoError(t, chainApp.BankKeeper.SendCoinsFromModuleToModule(ctx, utils.MintModule, uvalidatortypes.ModuleName, flushCoins))

	// Capture distribution balance before flush
	distrAddr := authtypes.NewModuleAddress(distrtypes.ModuleName)
	distrBefore := chainApp.BankKeeper.GetAllBalances(ctx, distrAddr).AmountOf(pchaintypes.BaseDenom)

	err := supplyslash.FlushUValidatorToDistribution(ctx, chainApp.BankKeeper)
	require.NoError(t, err)

	// uvalidator module should be drained
	uvAddr := authtypes.NewModuleAddress(uvalidatortypes.ModuleName)
	uvBal := chainApp.BankKeeper.GetAllBalances(ctx, uvAddr)
	require.True(t, uvBal.IsZero(), "uvalidator balance should be zero after flush")

	// distribution module should have received exactly flushAmt
	distrAfter := chainApp.BankKeeper.GetAllBalances(ctx, distrAddr).AmountOf(pchaintypes.BaseDenom)
	require.Equal(t, flushAmt, distrAfter.Sub(distrBefore),
		"distribution should gain exactly the flushed amount")
}

func TestFlushUValidatorToDistribution_Empty(t *testing.T) {
	chainApp := utils.SetupApp(t)
	ctx := chainApp.BaseApp.NewContext(true)

	// uvalidator has no balance — flush should be a no-op
	err := supplyslash.FlushUValidatorToDistribution(ctx, chainApp.BankKeeper)
	require.NoError(t, err)

	uvAddr := authtypes.NewModuleAddress(uvalidatortypes.ModuleName)
	require.True(t, chainApp.BankKeeper.GetAllBalances(ctx, uvAddr).IsZero())
}

func TestExecuteSlashEntries_BurnMath(t *testing.T) {
	chainApp := utils.SetupApp(t)
	ctx := chainApp.BaseApp.NewContext(true)

	// Create and fund an account with 1000 PC
	addr := sdk.AccAddress([]byte("test-slash-account-1"))
	acc := chainApp.AccountKeeper.NewAccountWithAddress(ctx, addr)
	chainApp.AccountKeeper.SetAccount(ctx, acc)

	fundAmt := sdkmath.NewInt(1000).Mul(onePCInUPC)
	fundCoins := sdk.NewCoins(sdk.NewCoin(pchaintypes.BaseDenom, fundAmt))
	require.NoError(t, chainApp.BankKeeper.MintCoins(ctx, utils.MintModule, fundCoins))
	require.NoError(t, chainApp.BankKeeper.SendCoinsFromModuleToAccount(ctx, utils.MintModule, addr, fundCoins))

	// Entry: current=1000 PC, target=100 PC → burn exactly 900 PC
	entries := []supplyslash.SlashEntry{
		{Address: addr.String(), AmountUPC: 1000.0, RebalancedTokens: 100.0},
	}

	totalBurned, success, skipped, errCount := supplyslash.ExecuteSlashEntries(ctx, chainApp.BankKeeper, entries)

	expectedBurn := sdkmath.NewInt(900).Mul(onePCInUPC)
	require.Equal(t, expectedBurn, totalBurned, "total burned should be 900 PC in upc")
	require.Equal(t, 1, success)
	require.Equal(t, 0, skipped)
	require.Equal(t, 0, errCount)

	// Account should have exactly 100 PC remaining
	remaining := chainApp.BankKeeper.GetAllBalances(ctx, addr).AmountOf(pchaintypes.BaseDenom)
	require.Equal(t, sdkmath.NewInt(100).Mul(onePCInUPC), remaining,
		"account should have 100 PC after slash")
}

func TestExecuteSlashEntries_SkipInsufficientBalance(t *testing.T) {
	chainApp := utils.SetupApp(t)
	ctx := chainApp.BaseApp.NewContext(true)

	// Fund account with only 50 PC
	addr := sdk.AccAddress([]byte("test-slash-account-2"))
	acc := chainApp.AccountKeeper.NewAccountWithAddress(ctx, addr)
	chainApp.AccountKeeper.SetAccount(ctx, acc)

	fundAmt := sdkmath.NewInt(50).Mul(onePCInUPC)
	fundCoins := sdk.NewCoins(sdk.NewCoin(pchaintypes.BaseDenom, fundAmt))
	require.NoError(t, chainApp.BankKeeper.MintCoins(ctx, utils.MintModule, fundCoins))
	require.NoError(t, chainApp.BankKeeper.SendCoinsFromModuleToAccount(ctx, utils.MintModule, addr, fundCoins))

	// Try to burn 1000 PC — far more than the account holds
	entries := []supplyslash.SlashEntry{
		{Address: addr.String(), AmountUPC: 1000.0, RebalancedTokens: 0.0},
	}

	totalBurned, success, skipped, errCount := supplyslash.ExecuteSlashEntries(ctx, chainApp.BankKeeper, entries)

	require.Equal(t, sdkmath.ZeroInt(), totalBurned, "nothing should be burned")
	require.Equal(t, 0, success)
	require.Equal(t, 1, skipped, "entry should be skipped, not errored")
	require.Equal(t, 0, errCount)

	// Account balance must be unchanged
	bal := chainApp.BankKeeper.GetAllBalances(ctx, addr).AmountOf(pchaintypes.BaseDenom)
	require.Equal(t, fundAmt, bal, "account balance should be unchanged when skipped")
}

func TestExecuteSlashEntries_SkipZeroBurn(t *testing.T) {
	chainApp := utils.SetupApp(t)
	ctx := chainApp.BaseApp.NewContext(true)

	addr := sdk.AccAddress([]byte("test-slash-account-3"))
	acc := chainApp.AccountKeeper.NewAccountWithAddress(ctx, addr)
	chainApp.AccountKeeper.SetAccount(ctx, acc)

	fundAmt := sdkmath.NewInt(100).Mul(onePCInUPC)
	fundCoins := sdk.NewCoins(sdk.NewCoin(pchaintypes.BaseDenom, fundAmt))
	require.NoError(t, chainApp.BankKeeper.MintCoins(ctx, utils.MintModule, fundCoins))
	require.NoError(t, chainApp.BankKeeper.SendCoinsFromModuleToAccount(ctx, utils.MintModule, addr, fundCoins))

	// amount_upc == rebalanced_tokens → burn = 0 → must be skipped
	entries := []supplyslash.SlashEntry{
		{Address: addr.String(), AmountUPC: 100.0, RebalancedTokens: 100.0},
	}

	totalBurned, success, skipped, errCount := supplyslash.ExecuteSlashEntries(ctx, chainApp.BankKeeper, entries)

	require.Equal(t, sdkmath.ZeroInt(), totalBurned)
	require.Equal(t, 0, success)
	require.Equal(t, 1, skipped)
	require.Equal(t, 0, errCount)

	bal := chainApp.BankKeeper.GetAllBalances(ctx, addr).AmountOf(pchaintypes.BaseDenom)
	require.Equal(t, fundAmt, bal, "balance should be unchanged when burn=0")
}

func TestExecuteSlashEntries_InvalidAddress(t *testing.T) {
	chainApp := utils.SetupApp(t)
	ctx := chainApp.BaseApp.NewContext(true)

	entries := []supplyslash.SlashEntry{
		{Address: "not-a-valid-bech32-address", AmountUPC: 1000.0, RebalancedTokens: 0.0},
	}

	totalBurned, success, skipped, errCount := supplyslash.ExecuteSlashEntries(ctx, chainApp.BankKeeper, entries)

	require.Equal(t, sdkmath.ZeroInt(), totalBurned)
	require.Equal(t, 0, success)
	require.Equal(t, 0, skipped)
	require.Equal(t, 1, errCount, "invalid bech32 address should be counted as an error")
}

func TestExecuteSlashEntries_MultipleEntries(t *testing.T) {
	chainApp := utils.SetupApp(t)
	ctx := chainApp.BaseApp.NewContext(true)

	// addr1: funded 1000 PC, burn 800 → keep 200
	addr1 := sdk.AccAddress([]byte("test-slash-account-4"))
	acc1 := chainApp.AccountKeeper.NewAccountWithAddress(ctx, addr1)
	chainApp.AccountKeeper.SetAccount(ctx, acc1)
	coins1 := sdk.NewCoins(sdk.NewCoin(pchaintypes.BaseDenom, sdkmath.NewInt(1000).Mul(onePCInUPC)))
	require.NoError(t, chainApp.BankKeeper.MintCoins(ctx, utils.MintModule, coins1))
	require.NoError(t, chainApp.BankKeeper.SendCoinsFromModuleToAccount(ctx, utils.MintModule, addr1, coins1))

	// addr2: funded 500 PC, burn 500 → keep 0 (exact drain)
	addr2 := sdk.AccAddress([]byte("test-slash-account-5"))
	acc2 := chainApp.AccountKeeper.NewAccountWithAddress(ctx, addr2)
	chainApp.AccountKeeper.SetAccount(ctx, acc2)
	coins2 := sdk.NewCoins(sdk.NewCoin(pchaintypes.BaseDenom, sdkmath.NewInt(500).Mul(onePCInUPC)))
	require.NoError(t, chainApp.BankKeeper.MintCoins(ctx, utils.MintModule, coins2))
	require.NoError(t, chainApp.BankKeeper.SendCoinsFromModuleToAccount(ctx, utils.MintModule, addr2, coins2))

	entries := []supplyslash.SlashEntry{
		{Address: addr1.String(), AmountUPC: 1000.0, RebalancedTokens: 200.0}, // burn 800 PC
		{Address: addr2.String(), AmountUPC: 500.0, RebalancedTokens: 0.0},    // burn 500 PC
		{Address: "bad-addr", AmountUPC: 100.0, RebalancedTokens: 0.0},        // invalid → error
		{Address: addr1.String(), AmountUPC: 50.0, RebalancedTokens: 50.0},    // burn=0 → skip
	}

	totalBurned, success, skipped, errCount := supplyslash.ExecuteSlashEntries(ctx, chainApp.BankKeeper, entries)

	// 800 + 500 = 1300 PC burned in total
	require.Equal(t, sdkmath.NewInt(1300).Mul(onePCInUPC), totalBurned)
	require.Equal(t, 2, success)
	require.Equal(t, 1, skipped)
	require.Equal(t, 1, errCount)

	// addr1 should have 200 PC left
	bal1 := chainApp.BankKeeper.GetAllBalances(ctx, addr1).AmountOf(pchaintypes.BaseDenom)
	require.Equal(t, sdkmath.NewInt(200).Mul(onePCInUPC), bal1)

	// addr2 should be empty
	bal2 := chainApp.BankKeeper.GetAllBalances(ctx, addr2).AmountOf(pchaintypes.BaseDenom)
	require.True(t, bal2.IsZero())
}
