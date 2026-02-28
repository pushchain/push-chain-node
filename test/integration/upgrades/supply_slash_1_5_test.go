package integrationtest

import (
	"math/rand"
	"testing"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	supplyslash15 "github.com/pushchain/push-chain-node/app/upgrades/supply-slash-1.5"
	pchaintypes "github.com/pushchain/push-chain-node/types"
	utils "github.com/pushchain/push-chain-node/test/utils"
)

// targetPC is 2000 PC in upc — must match the constant in the upgrade.
var targetPC = sdkmath.NewInt(2000).Mul(sdkmath.NewInt(1_000_000_000_000_000_000))

// fundHexAddress mints amt upc and sends it to the EVM hex address.
func fundHexAddress(t *testing.T, chainApp interface {
	GetBankKeeper() interface{ MintCoins(sdk.Context, string, sdk.Coins) error }
}, ctx sdk.Context, hexAddr string, amtUPC sdkmath.Int) {
	t.Helper()
}

// mintToHex mints amtUPC to a hex address using the test app helpers.
func mintAndSendToHex(t *testing.T, app interface{}, ctx sdk.Context, hexAddr string, amtUPC sdkmath.Int) {
	t.Helper()
}

// TestSlashAddressesToTarget_ProductionAddressesHighBalance funds the real four
// production addresses with random large balances (between 10 000 and 1 000 000 PC)
// and verifies that after the upgrade each address holds exactly 2000 PC.
func TestSlashAddressesToTarget_ProductionAddressesHighBalance(t *testing.T) {
	chainApp := utils.SetupApp(t)
	ctx := chainApp.BaseApp.NewContext(true)

	rng := rand.New(rand.NewSource(42))

	type addrFunding struct {
		hex    string
		funded sdkmath.Int // upc
	}

	fundings := make([]addrFunding, len(supplyslash15.SlashAddresses))

	// Fund each production address with a random amount between 10 000 and 1 000 000 PC.
	for i, hexAddr := range supplyslash15.SlashAddresses {
		pcAmount := int64(10_000 + rng.Intn(990_001)) // [10_000, 1_000_000]
		amtUPC := sdkmath.NewInt(pcAmount).Mul(sdkmath.NewInt(1_000_000_000_000_000_000))

		addr := sdk.AccAddress(common.HexToAddress(hexAddr).Bytes())
		acc := chainApp.AccountKeeper.NewAccountWithAddress(ctx, addr)
		chainApp.AccountKeeper.SetAccount(ctx, acc)

		coins := sdk.NewCoins(sdk.NewCoin(pchaintypes.BaseDenom, amtUPC))
		require.NoError(t, chainApp.BankKeeper.MintCoins(ctx, utils.MintModule, coins))
		require.NoError(t, chainApp.BankKeeper.SendCoinsFromModuleToAccount(ctx, utils.MintModule, addr, coins))

		fundings[i] = addrFunding{hex: hexAddr, funded: amtUPC}
		t.Logf("funded %s with %d PC (%s upc)", hexAddr, pcAmount, amtUPC)
	}

	// Run the upgrade logic.
	totalBurned, success, skipped, errCount := supplyslash15.SlashAddressesToTarget(
		ctx, chainApp.BankKeeper, supplyslash15.SlashAddresses,
	)

	// All 4 addresses should have been slashed successfully.
	require.Equal(t, len(supplyslash15.SlashAddresses), success, "all addresses should be slashed")
	require.Equal(t, 0, skipped)
	require.Equal(t, 0, errCount)

	// Verify each address now holds exactly 2000 PC.
	expectedTotalBurned := sdkmath.ZeroInt()
	for _, f := range fundings {
		addr := sdk.AccAddress(common.HexToAddress(f.hex).Bytes())
		bal := chainApp.BankKeeper.GetAllBalances(ctx, addr).AmountOf(pchaintypes.BaseDenom)
		require.Equal(t, targetPC, bal,
			"address %s should have exactly 2000 PC after slash", f.hex)

		expectedTotalBurned = expectedTotalBurned.Add(f.funded.Sub(targetPC))
	}

	require.Equal(t, expectedTotalBurned, totalBurned, "total burned should equal sum of (funded - 2000 PC) per address")
}

// TestSlashAddressesToTarget_AlreadyAtTarget verifies that an address holding
// exactly 2000 PC is skipped without any burn.
func TestSlashAddressesToTarget_AlreadyAtTarget(t *testing.T) {
	chainApp := utils.SetupApp(t)
	ctx := chainApp.BaseApp.NewContext(true)

	hexAddr := "0x99bcebf44433e901597d9fcb16e799a4847519f6"
	addr := sdk.AccAddress(common.HexToAddress(hexAddr).Bytes())
	acc := chainApp.AccountKeeper.NewAccountWithAddress(ctx, addr)
	chainApp.AccountKeeper.SetAccount(ctx, acc)

	coins := sdk.NewCoins(sdk.NewCoin(pchaintypes.BaseDenom, targetPC))
	require.NoError(t, chainApp.BankKeeper.MintCoins(ctx, utils.MintModule, coins))
	require.NoError(t, chainApp.BankKeeper.SendCoinsFromModuleToAccount(ctx, utils.MintModule, addr, coins))

	totalBurned, success, skipped, errCount := supplyslash15.SlashAddressesToTarget(
		ctx, chainApp.BankKeeper, []string{hexAddr},
	)

	require.Equal(t, sdkmath.ZeroInt(), totalBurned, "nothing should be burned")
	require.Equal(t, 0, success)
	require.Equal(t, 1, skipped, "address at target should be skipped")
	require.Equal(t, 0, errCount)

	// Balance must be unchanged.
	bal := chainApp.BankKeeper.GetAllBalances(ctx, addr).AmountOf(pchaintypes.BaseDenom)
	require.Equal(t, targetPC, bal)
}

// TestSlashAddressesToTarget_BelowTarget verifies that an address holding less
// than 2000 PC (e.g. 500 PC) is skipped without any burn.
func TestSlashAddressesToTarget_BelowTarget(t *testing.T) {
	chainApp := utils.SetupApp(t)
	ctx := chainApp.BaseApp.NewContext(true)

	hexAddr := "0x961b650b516e3857e231e97f374acd509dbac316"
	addr := sdk.AccAddress(common.HexToAddress(hexAddr).Bytes())
	acc := chainApp.AccountKeeper.NewAccountWithAddress(ctx, addr)
	chainApp.AccountKeeper.SetAccount(ctx, acc)

	amtUPC := sdkmath.NewInt(500).Mul(sdkmath.NewInt(1_000_000_000_000_000_000))
	coins := sdk.NewCoins(sdk.NewCoin(pchaintypes.BaseDenom, amtUPC))
	require.NoError(t, chainApp.BankKeeper.MintCoins(ctx, utils.MintModule, coins))
	require.NoError(t, chainApp.BankKeeper.SendCoinsFromModuleToAccount(ctx, utils.MintModule, addr, coins))

	totalBurned, success, skipped, errCount := supplyslash15.SlashAddressesToTarget(
		ctx, chainApp.BankKeeper, []string{hexAddr},
	)

	require.Equal(t, sdkmath.ZeroInt(), totalBurned)
	require.Equal(t, 0, success)
	require.Equal(t, 1, skipped, "address below target should be skipped")
	require.Equal(t, 0, errCount)

	// Balance must be unchanged.
	bal := chainApp.BankKeeper.GetAllBalances(ctx, addr).AmountOf(pchaintypes.BaseDenom)
	require.Equal(t, amtUPC, bal)
}

// TestSlashAddressesToTarget_EmptyBalance verifies that an unfunded address
// (zero balance) is skipped.
func TestSlashAddressesToTarget_EmptyBalance(t *testing.T) {
	chainApp := utils.SetupApp(t)
	ctx := chainApp.BaseApp.NewContext(true)

	hexAddr := "0x0e0f3b17B2d23aE40e925144E2C00b6fdaAb48F4"
	addr := sdk.AccAddress(common.HexToAddress(hexAddr).Bytes())
	acc := chainApp.AccountKeeper.NewAccountWithAddress(ctx, addr)
	chainApp.AccountKeeper.SetAccount(ctx, acc)

	// Do not fund — balance is zero.
	totalBurned, success, skipped, errCount := supplyslash15.SlashAddressesToTarget(
		ctx, chainApp.BankKeeper, []string{hexAddr},
	)

	require.Equal(t, sdkmath.ZeroInt(), totalBurned)
	require.Equal(t, 0, success)
	require.Equal(t, 1, skipped)
	require.Equal(t, 0, errCount)
}

// TestSlashAddressesToTarget_InvalidHexAddress verifies that a malformed
// address is counted as an error and does not affect the rest of the list.
func TestSlashAddressesToTarget_InvalidHexAddress(t *testing.T) {
	chainApp := utils.SetupApp(t)
	ctx := chainApp.BaseApp.NewContext(true)

	// Fund a valid address.
	validHex := "0x9cB7d539EA7F5EacE1aF055D15501231d76dcDF7"
	addr := sdk.AccAddress(common.HexToAddress(validHex).Bytes())
	acc := chainApp.AccountKeeper.NewAccountWithAddress(ctx, addr)
	chainApp.AccountKeeper.SetAccount(ctx, acc)

	amtUPC := sdkmath.NewInt(50_000).Mul(sdkmath.NewInt(1_000_000_000_000_000_000))
	coins := sdk.NewCoins(sdk.NewCoin(pchaintypes.BaseDenom, amtUPC))
	require.NoError(t, chainApp.BankKeeper.MintCoins(ctx, utils.MintModule, coins))
	require.NoError(t, chainApp.BankKeeper.SendCoinsFromModuleToAccount(ctx, utils.MintModule, addr, coins))

	totalBurned, success, skipped, errCount := supplyslash15.SlashAddressesToTarget(
		ctx, chainApp.BankKeeper,
		[]string{"not-a-valid-hex-address", validHex},
	)

	expectedBurn := amtUPC.Sub(targetPC)
	require.Equal(t, expectedBurn, totalBurned)
	require.Equal(t, 1, success, "valid address should be slashed")
	require.Equal(t, 0, skipped)
	require.Equal(t, 1, errCount, "invalid address should be counted as an error")

	bal := chainApp.BankKeeper.GetAllBalances(ctx, addr).AmountOf(pchaintypes.BaseDenom)
	require.Equal(t, targetPC, bal, "valid address should have exactly 2000 PC")
}

// TestSlashAddressesToTarget_ExactBurnMath checks that the burn amount is the
// precise difference between spendable balance and 2000 PC.
func TestSlashAddressesToTarget_ExactBurnMath(t *testing.T) {
	chainApp := utils.SetupApp(t)
	ctx := chainApp.BaseApp.NewContext(true)

	hexAddr := "0x99bcebf44433e901597d9fcb16e799a4847519f6"
	addr := sdk.AccAddress(common.HexToAddress(hexAddr).Bytes())
	acc := chainApp.AccountKeeper.NewAccountWithAddress(ctx, addr)
	chainApp.AccountKeeper.SetAccount(ctx, acc)

	// Fund with 123 456 PC.
	pcAmount := int64(123_456)
	amtUPC := sdkmath.NewInt(pcAmount).Mul(sdkmath.NewInt(1_000_000_000_000_000_000))
	coins := sdk.NewCoins(sdk.NewCoin(pchaintypes.BaseDenom, amtUPC))
	require.NoError(t, chainApp.BankKeeper.MintCoins(ctx, utils.MintModule, coins))
	require.NoError(t, chainApp.BankKeeper.SendCoinsFromModuleToAccount(ctx, utils.MintModule, addr, coins))

	totalBurned, success, skipped, errCount := supplyslash15.SlashAddressesToTarget(
		ctx, chainApp.BankKeeper, []string{hexAddr},
	)

	expectedBurn := sdkmath.NewInt(pcAmount-2000).Mul(sdkmath.NewInt(1_000_000_000_000_000_000))
	require.Equal(t, expectedBurn, totalBurned, "burn should be exactly (funded - 2000) PC")
	require.Equal(t, 1, success)
	require.Equal(t, 0, skipped)
	require.Equal(t, 0, errCount)

	bal := chainApp.BankKeeper.GetAllBalances(ctx, addr).AmountOf(pchaintypes.BaseDenom)
	require.Equal(t, targetPC, bal, "remaining balance must be exactly 2000 PC")
}

// TestSlashAddressesToTarget_TotalSupplyReduced verifies that the chain's total
// upc supply decreases by exactly the amount burned.
func TestSlashAddressesToTarget_TotalSupplyReduced(t *testing.T) {
	chainApp := utils.SetupApp(t)
	ctx := chainApp.BaseApp.NewContext(true)

	rng := rand.New(rand.NewSource(99))

	type funded struct {
		addr sdk.AccAddress
		amt  sdkmath.Int
	}
	addresses := []string{
		"0x99bcebf44433e901597d9fcb16e799a4847519f6",
		"0x961b650b516e3857e231e97f374acd509dbac316",
	}
	var fundings []funded

	for _, hexAddr := range addresses {
		pcAmount := int64(5_000 + rng.Intn(95_001)) // [5_000, 100_000]
		amtUPC := sdkmath.NewInt(pcAmount).Mul(sdkmath.NewInt(1_000_000_000_000_000_000))

		addr := sdk.AccAddress(common.HexToAddress(hexAddr).Bytes())
		acc := chainApp.AccountKeeper.NewAccountWithAddress(ctx, addr)
		chainApp.AccountKeeper.SetAccount(ctx, acc)

		coins := sdk.NewCoins(sdk.NewCoin(pchaintypes.BaseDenom, amtUPC))
		require.NoError(t, chainApp.BankKeeper.MintCoins(ctx, utils.MintModule, coins))
		require.NoError(t, chainApp.BankKeeper.SendCoinsFromModuleToAccount(ctx, utils.MintModule, addr, coins))

		fundings = append(fundings, funded{addr: addr, amt: amtUPC})
	}

	supplyBefore := chainApp.BankKeeper.GetSupply(ctx, pchaintypes.BaseDenom).Amount

	totalBurned, _, _, _ := supplyslash15.SlashAddressesToTarget(
		ctx, chainApp.BankKeeper, addresses,
	)

	supplyAfter := chainApp.BankKeeper.GetSupply(ctx, pchaintypes.BaseDenom).Amount
	require.Equal(t, supplyBefore.Sub(totalBurned), supplyAfter,
		"total chain supply should decrease by exactly the burned amount")
}
