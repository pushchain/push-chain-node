package integrationtest

import (
	"testing"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	evmtypes "github.com/cosmos/evm/x/vm/types"
	utils "github.com/pushchain/push-chain-node/test/utils"
	pchaintypes "github.com/pushchain/push-chain-node/types"
	ucallbacktypes "github.com/pushchain/push-chain-node/x/ucallback/types"
)

// The address UniversalCallback hardcodes in its access control. Derived from the
// module name, so it is fixed for the life of the chain — but derived by the real
// account keeper here, not recomputed by the test.
const expectedModuleEVMAddr = "0x07a0258D367A4A4cd9d6E4b7eEE8E7eF491CC519"

// The module account must exist on a real app, and resolve to the address the
// contract admits. A unit test with a fake account keeper cannot show this: it
// would return whatever the fake was told to.
func TestModuleAccount_ExistsAndMatchesTheContract(t *testing.T) {
	chainApp, ctx, _ := utils.SetAppWithValidators(t)
	k := chainApp.UcallbackKeeper

	evmAddr, hexAddr := k.GetModuleAddress(ctx)
	require.Equal(t, expectedModuleEVMAddr, hexAddr,
		"UniversalCallback.sol hardcodes this; a change here breaks every fulfil and expire")

	acc := chainApp.AccountKeeper.GetModuleAccount(ctx, ucallbacktypes.ModuleName)
	require.NotNil(t, acc, "the module account must be provisioned")
	require.Equal(t, acc.GetAddress().Bytes(), evmAddr.Bytes(),
		"the EVM address is the cosmos address's 20 bytes")
}

// The module must hold Burner. Without it BurnCoins fails at the bank keeper, and
// no amount of unit testing with a fake bank would reveal it.
func TestModuleAccount_HasBurnerPermission(t *testing.T) {
	chainApp, ctx, _ := utils.SetAppWithValidators(t)

	acc := chainApp.AccountKeeper.GetModuleAccount(ctx, ucallbacktypes.ModuleName)
	modAcc, ok := acc.(sdk.ModuleAccountI)
	require.True(t, ok)
	require.True(t, modAcc.HasPermission(authtypes.Burner),
		"x/ucallback burns consumed callback gas")
	require.False(t, modAcc.HasPermission(authtypes.Minter),
		"it must never be able to create supply")
}

// The module receives the consumed budget out of UniversalCallback before burning
// it, so it must not be in the blocked set. Every maccPerms entry is blocked by
// default — this asserts the deliberate exemption is still there.
func TestModuleAccount_CanReceiveFunds(t *testing.T) {
	chainApp, ctx, _ := utils.SetAppWithValidators(t)

	modAddr := authtypes.NewModuleAddress(ucallbacktypes.ModuleName)
	require.False(t, chainApp.BankKeeper.BlockedAddr(modAddr),
		"a blocked module account cannot be sent the escrow it must burn")

	// and prove it end to end against the real bank
	funder := sdk.AccAddress(common.HexToAddress("0xBEEF").Bytes())
	coins := sdk.NewCoins(sdk.NewCoin(pchaintypes.BaseDenom, sdkmath.NewInt(1_000)))
	require.NoError(t, chainApp.BankKeeper.MintCoins(ctx, evmtypes.ModuleName, coins))
	require.NoError(t, chainApp.BankKeeper.SendCoinsFromModuleToAccount(
		ctx, evmtypes.ModuleName, funder, coins))

	require.NoError(t, chainApp.BankKeeper.SendCoinsFromAccountToModule(
		ctx, funder, ucallbacktypes.ModuleName, coins))

	got := chainApp.BankKeeper.GetBalance(ctx,
		authtypes.NewModuleAddress(ucallbacktypes.ModuleName), pchaintypes.BaseDenom)
	require.Equal(t, sdkmath.NewInt(1_000), got.Amount)
}
