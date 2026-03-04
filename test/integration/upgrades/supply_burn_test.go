package integrationtest

import (
	"testing"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	supplyburn "github.com/pushchain/push-chain-node/app/upgrades/supply-burn"
	pchaintypes "github.com/pushchain/push-chain-node/types"
	utils "github.com/pushchain/push-chain-node/test/utils"
)

func TestExecuteBurnEntries_BurnAll(t *testing.T) {
	chainApp := utils.SetupApp(t)
	ctx := chainApp.BaseApp.NewContext(true)

	// Fund account with 1000 PC, keep_pc=0 → burn entire spendable balance
	addr := sdk.AccAddress([]byte("test-burn-account-1 "))
	acc := chainApp.AccountKeeper.NewAccountWithAddress(ctx, addr)
	chainApp.AccountKeeper.SetAccount(ctx, acc)

	fundAmt := sdkmath.NewInt(1000).Mul(onePCInUPC)
	fundCoins := sdk.NewCoins(sdk.NewCoin(pchaintypes.BaseDenom, fundAmt))
	require.NoError(t, chainApp.BankKeeper.MintCoins(ctx, utils.MintModule, fundCoins))
	require.NoError(t, chainApp.BankKeeper.SendCoinsFromModuleToAccount(ctx, utils.MintModule, addr, fundCoins))

	entries := []supplyburn.BurnEntry{
		{Address: addr.String(), KeepPC: 0},
	}

	totalBurned, success, skipped, errCount := supplyburn.ExecuteBurnEntries(ctx, chainApp.BankKeeper, entries)

	require.Equal(t, fundAmt, totalBurned, "should burn the entire 1000 PC")
	require.Equal(t, 1, success)
	require.Equal(t, 0, skipped)
	require.Equal(t, 0, errCount)

	// Account must be empty
	bal := chainApp.BankKeeper.GetAllBalances(ctx, addr).AmountOf(pchaintypes.BaseDenom)
	require.True(t, bal.IsZero(), "account should be empty after burn-all")
}

func TestExecuteBurnEntries_KeepPartial(t *testing.T) {
	chainApp := utils.SetupApp(t)
	ctx := chainApp.BaseApp.NewContext(true)

	// Fund account with 1000 PC, keep_pc=100 → burn 900 PC
	addr := sdk.AccAddress([]byte("test-burn-account-2 "))
	acc := chainApp.AccountKeeper.NewAccountWithAddress(ctx, addr)
	chainApp.AccountKeeper.SetAccount(ctx, acc)

	fundAmt := sdkmath.NewInt(1000).Mul(onePCInUPC)
	fundCoins := sdk.NewCoins(sdk.NewCoin(pchaintypes.BaseDenom, fundAmt))
	require.NoError(t, chainApp.BankKeeper.MintCoins(ctx, utils.MintModule, fundCoins))
	require.NoError(t, chainApp.BankKeeper.SendCoinsFromModuleToAccount(ctx, utils.MintModule, addr, fundCoins))

	entries := []supplyburn.BurnEntry{
		{Address: addr.String(), KeepPC: 100},
	}

	totalBurned, success, skipped, errCount := supplyburn.ExecuteBurnEntries(ctx, chainApp.BankKeeper, entries)

	expectedBurn := sdkmath.NewInt(900).Mul(onePCInUPC)
	require.Equal(t, expectedBurn, totalBurned, "should burn exactly 900 PC")
	require.Equal(t, 1, success)
	require.Equal(t, 0, skipped)
	require.Equal(t, 0, errCount)

	// Account must have exactly 100 PC
	bal := chainApp.BankKeeper.GetAllBalances(ctx, addr).AmountOf(pchaintypes.BaseDenom)
	require.Equal(t, sdkmath.NewInt(100).Mul(onePCInUPC), bal,
		"account should have exactly 100 PC remaining")
}

func TestExecuteBurnEntries_SkipWhenSpendableLEKeep(t *testing.T) {
	chainApp := utils.SetupApp(t)
	ctx := chainApp.BaseApp.NewContext(true)

	// Fund account with 50 PC, keep_pc=100 → spendable <= keep → skip
	addr := sdk.AccAddress([]byte("test-burn-account-3 "))
	acc := chainApp.AccountKeeper.NewAccountWithAddress(ctx, addr)
	chainApp.AccountKeeper.SetAccount(ctx, acc)

	fundAmt := sdkmath.NewInt(50).Mul(onePCInUPC)
	fundCoins := sdk.NewCoins(sdk.NewCoin(pchaintypes.BaseDenom, fundAmt))
	require.NoError(t, chainApp.BankKeeper.MintCoins(ctx, utils.MintModule, fundCoins))
	require.NoError(t, chainApp.BankKeeper.SendCoinsFromModuleToAccount(ctx, utils.MintModule, addr, fundCoins))

	entries := []supplyburn.BurnEntry{
		{Address: addr.String(), KeepPC: 100},
	}

	totalBurned, success, skipped, errCount := supplyburn.ExecuteBurnEntries(ctx, chainApp.BankKeeper, entries)

	require.Equal(t, sdkmath.ZeroInt(), totalBurned, "nothing should be burned")
	require.Equal(t, 0, success)
	require.Equal(t, 1, skipped, "entry should be skipped, not errored")
	require.Equal(t, 0, errCount)

	// Balance must be unchanged
	bal := chainApp.BankKeeper.GetAllBalances(ctx, addr).AmountOf(pchaintypes.BaseDenom)
	require.Equal(t, fundAmt, bal, "balance should be unchanged when skipped")
}

func TestExecuteBurnEntries_SkipZeroBalance(t *testing.T) {
	chainApp := utils.SetupApp(t)
	ctx := chainApp.BaseApp.NewContext(true)

	// Account exists but has no funds, keep_pc=0 → skip (nothing to burn)
	addr := sdk.AccAddress([]byte("test-burn-account-4 "))
	acc := chainApp.AccountKeeper.NewAccountWithAddress(ctx, addr)
	chainApp.AccountKeeper.SetAccount(ctx, acc)

	entries := []supplyburn.BurnEntry{
		{Address: addr.String(), KeepPC: 0},
	}

	totalBurned, success, skipped, errCount := supplyburn.ExecuteBurnEntries(ctx, chainApp.BankKeeper, entries)

	require.Equal(t, sdkmath.ZeroInt(), totalBurned)
	require.Equal(t, 0, success)
	require.Equal(t, 1, skipped)
	require.Equal(t, 0, errCount)
}

func TestExecuteBurnEntries_InvalidAddress(t *testing.T) {
	chainApp := utils.SetupApp(t)
	ctx := chainApp.BaseApp.NewContext(true)

	entries := []supplyburn.BurnEntry{
		{Address: "not-a-valid-bech32-address", KeepPC: 0},
	}

	totalBurned, success, skipped, errCount := supplyburn.ExecuteBurnEntries(ctx, chainApp.BankKeeper, entries)

	require.Equal(t, sdkmath.ZeroInt(), totalBurned)
	require.Equal(t, 0, success)
	require.Equal(t, 0, skipped)
	require.Equal(t, 1, errCount, "invalid bech32 address should be counted as an error")
}

func TestExecuteBurnEntries_MultipleEntries(t *testing.T) {
	chainApp := utils.SetupApp(t)
	ctx := chainApp.BaseApp.NewContext(true)

	// addr1: 1000 PC, keep=0 → burn 1000 PC
	addr1 := sdk.AccAddress([]byte("test-burn-account-5 "))
	acc1 := chainApp.AccountKeeper.NewAccountWithAddress(ctx, addr1)
	chainApp.AccountKeeper.SetAccount(ctx, acc1)
	coins1 := sdk.NewCoins(sdk.NewCoin(pchaintypes.BaseDenom, sdkmath.NewInt(1000).Mul(onePCInUPC)))
	require.NoError(t, chainApp.BankKeeper.MintCoins(ctx, utils.MintModule, coins1))
	require.NoError(t, chainApp.BankKeeper.SendCoinsFromModuleToAccount(ctx, utils.MintModule, addr1, coins1))

	// addr2: 500 PC, keep=200 → burn 300 PC
	addr2 := sdk.AccAddress([]byte("test-burn-account-6 "))
	acc2 := chainApp.AccountKeeper.NewAccountWithAddress(ctx, addr2)
	chainApp.AccountKeeper.SetAccount(ctx, acc2)
	coins2 := sdk.NewCoins(sdk.NewCoin(pchaintypes.BaseDenom, sdkmath.NewInt(500).Mul(onePCInUPC)))
	require.NoError(t, chainApp.BankKeeper.MintCoins(ctx, utils.MintModule, coins2))
	require.NoError(t, chainApp.BankKeeper.SendCoinsFromModuleToAccount(ctx, utils.MintModule, addr2, coins2))

	// addr3: 50 PC, keep=100 → spendable <= keep → skip
	addr3 := sdk.AccAddress([]byte("test-burn-account-7 "))
	acc3 := chainApp.AccountKeeper.NewAccountWithAddress(ctx, addr3)
	chainApp.AccountKeeper.SetAccount(ctx, acc3)
	coins3 := sdk.NewCoins(sdk.NewCoin(pchaintypes.BaseDenom, sdkmath.NewInt(50).Mul(onePCInUPC)))
	require.NoError(t, chainApp.BankKeeper.MintCoins(ctx, utils.MintModule, coins3))
	require.NoError(t, chainApp.BankKeeper.SendCoinsFromModuleToAccount(ctx, utils.MintModule, addr3, coins3))

	entries := []supplyburn.BurnEntry{
		{Address: addr1.String(), KeepPC: 0},            // burn 1000 PC
		{Address: addr2.String(), KeepPC: 200},          // burn 300 PC
		{Address: addr3.String(), KeepPC: 100},          // skip (50 < 100)
		{Address: "bad-addr", KeepPC: 0},                // invalid → error
	}

	totalBurned, success, skipped, errCount := supplyburn.ExecuteBurnEntries(ctx, chainApp.BankKeeper, entries)

	// 1000 + 300 = 1300 PC burned
	require.Equal(t, sdkmath.NewInt(1300).Mul(onePCInUPC), totalBurned)
	require.Equal(t, 2, success)
	require.Equal(t, 1, skipped)
	require.Equal(t, 1, errCount)

	// addr1 must be empty
	bal1 := chainApp.BankKeeper.GetAllBalances(ctx, addr1).AmountOf(pchaintypes.BaseDenom)
	require.True(t, bal1.IsZero(), "addr1 should be fully burned")

	// addr2 must have exactly 200 PC
	bal2 := chainApp.BankKeeper.GetAllBalances(ctx, addr2).AmountOf(pchaintypes.BaseDenom)
	require.Equal(t, sdkmath.NewInt(200).Mul(onePCInUPC), bal2, "addr2 should keep 200 PC")

	// addr3 must be unchanged (50 PC)
	bal3 := chainApp.BankKeeper.GetAllBalances(ctx, addr3).AmountOf(pchaintypes.BaseDenom)
	require.Equal(t, sdkmath.NewInt(50).Mul(onePCInUPC), bal3, "addr3 should be unchanged")
}

func TestExecuteBurnEntries_ExactKeepEqualsSpendable(t *testing.T) {
	chainApp := utils.SetupApp(t)
	ctx := chainApp.BaseApp.NewContext(true)

	// Fund 100 PC, keep=100 → spendable == keep → skip (nothing to burn)
	addr := sdk.AccAddress([]byte("test-burn-account-8 "))
	acc := chainApp.AccountKeeper.NewAccountWithAddress(ctx, addr)
	chainApp.AccountKeeper.SetAccount(ctx, acc)

	fundAmt := sdkmath.NewInt(100).Mul(onePCInUPC)
	fundCoins := sdk.NewCoins(sdk.NewCoin(pchaintypes.BaseDenom, fundAmt))
	require.NoError(t, chainApp.BankKeeper.MintCoins(ctx, utils.MintModule, fundCoins))
	require.NoError(t, chainApp.BankKeeper.SendCoinsFromModuleToAccount(ctx, utils.MintModule, addr, fundCoins))

	entries := []supplyburn.BurnEntry{
		{Address: addr.String(), KeepPC: 100},
	}

	totalBurned, success, skipped, errCount := supplyburn.ExecuteBurnEntries(ctx, chainApp.BankKeeper, entries)

	require.Equal(t, sdkmath.ZeroInt(), totalBurned)
	require.Equal(t, 0, success)
	require.Equal(t, 1, skipped, "equal spendable and keep should be skipped")
	require.Equal(t, 0, errCount)

	bal := chainApp.BankKeeper.GetAllBalances(ctx, addr).AmountOf(pchaintypes.BaseDenom)
	require.Equal(t, fundAmt, bal, "balance should be unchanged")
}
