package integrationtest

import (
	"math/big"
	"testing"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	utils "github.com/pushchain/push-chain-node/test/utils"
	pchaintypes "github.com/pushchain/push-chain-node/types"
	uregistrytypes "github.com/pushchain/push-chain-node/x/uregistry/types"
)

func callbackContractAddr() sdk.AccAddress {
	return sdk.AccAddress(common.HexToAddress(
		uregistrytypes.SYSTEM_CONTRACTS["UNIVERSAL_CALLBACK"].Address).Bytes())
}

// TakeAndBurn must genuinely reduce total supply, not move coins somewhere.
//
// This is the claim a fake bank cannot make: it needs the real Burner permission,
// the real blocked-address exemption, and the real supply accounting all agreeing.
func TestTakeAndBurn_ReducesTotalSupply(t *testing.T) {
	chainApp, ctx, _ := utils.SetAppWithValidators(t)
	k := chainApp.UcallbackKeeper
	bank := chainApp.BankKeeper

	// escrow sitting on UniversalCallback, as it would be after a request
	escrow := sdkmath.NewInt(5_000_000_000_000_000)
	coins := sdk.NewCoins(sdk.NewCoin(pchaintypes.BaseDenom, escrow))
	require.NoError(t, bank.MintCoins(ctx, evmtypes.ModuleName, coins))
	require.NoError(t, bank.SendCoinsFromModuleToAccount(
		ctx, evmtypes.ModuleName, callbackContractAddr(), coins))

	supplyBefore := bank.GetSupply(ctx, pchaintypes.BaseDenom).Amount
	contractBefore := bank.GetBalance(ctx, callbackContractAddr(), pchaintypes.BaseDenom).Amount
	require.Equal(t, escrow, contractBefore)

	burn := big.NewInt(2_000_000_000_000_000)
	require.NoError(t, k.TakeAndBurn(ctx, burn))

	supplyAfter := bank.GetSupply(ctx, pchaintypes.BaseDenom).Amount
	contractAfter := bank.GetBalance(ctx, callbackContractAddr(), pchaintypes.BaseDenom).Amount

	require.Equal(t, sdkmath.NewIntFromBigInt(burn), supplyBefore.Sub(supplyAfter),
		"supply must fall by exactly the burned amount")
	require.Equal(t, sdkmath.NewIntFromBigInt(burn), contractBefore.Sub(contractAfter),
		"and it must come out of the contract, not anywhere else")

	// nothing may be left parked in the module — it takes and burns in one step
	modAddr, _ := k.GetModuleAddress(ctx)
	modBal := bank.GetBalance(ctx, sdk.AccAddress(modAddr.Bytes()), pchaintypes.BaseDenom)
	require.True(t, modBal.Amount.IsZero(), "the module must not retain what it burned")
}

// A burn larger than the contract holds must fail cleanly and destroy nothing —
// the affordability gate should prevent it, so this is the backstop.
func TestTakeAndBurn_FailsWithoutSufficientEscrow(t *testing.T) {
	chainApp, ctx, _ := utils.SetAppWithValidators(t)
	k := chainApp.UcallbackKeeper
	bank := chainApp.BankKeeper

	supplyBefore := bank.GetSupply(ctx, pchaintypes.BaseDenom).Amount

	err := k.TakeAndBurn(ctx, big.NewInt(1_000_000))
	require.Error(t, err, "cannot take escrow the contract does not hold")

	require.Equal(t, supplyBefore, bank.GetSupply(ctx, pchaintypes.BaseDenom).Amount,
		"a failed take must burn nothing")
}

// Zero and nil are no-ops, not errors — a callback that consumed nothing settles
// without a burn.
func TestTakeAndBurn_ZeroIsANoop(t *testing.T) {
	chainApp, ctx, _ := utils.SetAppWithValidators(t)
	k := chainApp.UcallbackKeeper

	before := chainApp.BankKeeper.GetSupply(ctx, pchaintypes.BaseDenom).Amount
	require.NoError(t, k.TakeAndBurn(ctx, big.NewInt(0)))
	require.NoError(t, k.TakeAndBurn(ctx, nil))
	require.Equal(t, before, chainApp.BankKeeper.GetSupply(ctx, pchaintypes.BaseDenom).Amount)
}

// CallbackCost must price against the chain's real base fee, not a fixture.
func TestCallbackCost_UsesTheChainBaseFee(t *testing.T) {
	chainApp, ctx, _ := utils.SetAppWithValidators(t)
	k := chainApp.UcallbackKeeper

	// The bare harness leaves feemarket params unset; pin a base fee the way the
	// uexecutor integration tests do, then read it back through the same keeper
	// x/ucallback uses in production.
	params := chainApp.FeeMarketKeeper.GetParams(ctx)
	params.BaseFee = sdkmath.LegacyNewDec(1_000_000_000)
	require.NoError(t, chainApp.FeeMarketKeeper.SetParams(ctx, params))

	baseFee := chainApp.FeeMarketKeeper.GetBaseFee(ctx)
	require.False(t, baseFee.IsNil(), "the chain must expose a base fee")
	require.Equal(t, sdkmath.LegacyNewDec(1_000_000_000), baseFee)

	cost, err := k.CallbackCost(ctx, 100_000)
	require.NoError(t, err)

	want := new(big.Int).Mul(big.NewInt(100_000), baseFee.TruncateInt().BigInt())
	require.Equal(t, want, cost)
}
