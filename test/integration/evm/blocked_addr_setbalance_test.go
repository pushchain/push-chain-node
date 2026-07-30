package integrationtest

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"

	"github.com/pushchain/push-chain-node/app"
	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"

	utils "github.com/pushchain/push-chain-node/test/utils"
)

// cosmosAddrStr renders an EVM address the way BlockedAddresses() keys it (bech32).
func cosmosAddrStr(a common.Address) string { return sdk.AccAddress(a.Bytes()).String() }

// cosmos/evm v0.7.0 added a blocked-address guard to Keeper.SetBalance
// (upstream `feat!: Krakatoa` #1030, x/vm/keeper/statedb.go):
//
//	if amount > currentSpendable && k.bankWrapper.BlockedAddr(cosmosAddr) {
//	    return ErrUnauthorized "%s is not allowed to receive funds"
//	}
//
// v0.6 had no such check — it simply mint/burned the delta. The guard is therefore
// consensus-visible: an EVM value transfer that succeeded on v0.6 can now fail.
//
// The list it consults is this app's BlockedAddresses(), wired into the bank keeper
// in app.go. In v0.6 that list only governed bank sends; from v0.7 it also governs
// EVM balance increases, so its exact contents now matter far more.
//
// These tests pin the real behaviour so a future change to BlockedAddresses() — or to
// upstream's guard — fails loudly rather than silently altering which EVM transfers
// are permitted.

// TestSetBalanceRejectsIncreaseToBlockedAddresses asserts the guard fires for the two
// categories BlockedAddresses() covers: module accounts and native precompiles.
func TestSetBalanceRejectsIncreaseToBlockedAddresses(t *testing.T) {
	chainApp := utils.SetupApp(t)
	ctx := chainApp.BaseApp.NewContext(true)

	blocked := app.BlockedAddresses()

	t.Run("module account (uexecutor) is blocked", func(t *testing.T) {
		modAcc := authtypes.NewModuleAddress(uexecutortypes.ModuleName)
		require.True(t, blocked[modAcc.String()], "uexecutor module account must be in BlockedAddresses()")

		err := chainApp.EVMKeeper.SetBalance(ctx, common.BytesToAddress(modAcc), uint256.NewInt(1_000_000))
		require.Error(t, err, "v0.7 must refuse an EVM balance increase to a blocked module account")
		require.Contains(t, err.Error(), "not allowed to receive funds")
	})

	t.Run("native precompile 0x01 (ecrecover) is blocked", func(t *testing.T) {
		addr := common.HexToAddress("0x0000000000000000000000000000000000000001")
		require.True(t, blocked[cosmosAddrStr(addr)], "0x01 must be in BlockedAddresses()")

		err := chainApp.EVMKeeper.SetBalance(ctx, addr, uint256.NewInt(1_000_000))
		require.Error(t, err, "v0.7 must refuse an EVM balance increase to a native precompile")
		require.Contains(t, err.Error(), "not allowed to receive funds")
	})

	t.Run("a plain EOA is NOT blocked", func(t *testing.T) {
		// Control: proves the guard is selective, not blanket.
		addr := common.HexToAddress("0x00000000000000000000000000000000000000ff")
		require.False(t, blocked[cosmosAddrStr(addr)])

		require.NoError(t, chainApp.EVMKeeper.SetBalance(ctx, addr, uint256.NewInt(1_000_000)),
			"an ordinary account must still be able to receive funds")
	})
}

// TestBlockedAddressesCoverage documents the composition of BlockedAddresses(), including
// two deliberate divergences from upstream's reference (evmd/config/permissions.go).
func TestBlockedAddressesCoverage(t *testing.T) {
	blocked := app.BlockedAddresses()

	t.Run("gov module account is deliberately NOT blocked", func(t *testing.T) {
		// push-chain removes gov from the set (upstream keeps it blocked) so the gov
		// module can receive deposits.
		gov := authtypes.NewModuleAddress(govtypes.ModuleName)
		require.False(t, blocked[gov.String()], "gov is intentionally unblocked on push-chain")
	})

	t.Run("cosmos-evm static precompiles are blocked", func(t *testing.T) {
		require.NotEmpty(t, evmtypes.AvailableStaticPrecompiles)
		for _, p := range evmtypes.AvailableStaticPrecompiles {
			require.True(t, blocked[cosmosAddrStr(common.HexToAddress(p))],
				"static precompile %s must be blocked", p)
		}
	})

	// The native-precompile portion now uses PrecompiledAddressesPrague (0x01–0x11),
	// matching upstream evmd. It previously used the Berlin set (0x01–0x09), leaving
	// 0x0a (KZG point evaluation) through 0x11 (BLS12-381) able to receive funds that
	// could never be spent again. All eight were verified empty on live donut (zero
	// balance, zero nonce) before being blocked, so nothing in flight was affected.
	t.Run("all Prague native precompiles are blocked", func(t *testing.T) {
		for _, hex := range []string{
			"0x0000000000000000000000000000000000000001", // ecrecover      (Berlin)
			"0x0000000000000000000000000000000000000009", // blake2F        (Berlin)
			"0x000000000000000000000000000000000000000a", // KZG            (Cancun)
			"0x000000000000000000000000000000000000000b", // BLS12-381 G1Add
			"0x0000000000000000000000000000000000000011", // BLS12-381 MapG2 (Prague)
		} {
			require.True(t, blocked[cosmosAddrStr(common.HexToAddress(hex))],
				"native precompile %s must be blocked so funds cannot be burnt there", hex)
		}
	})

	// Guards the change above: widening the precompile set must not disturb which
	// MODULE accounts can receive funds. Every module in maccPerms stays blocked, and
	// gov stays the sole exception.
	t.Run("module-account blocking is unchanged by the precompile widening", func(t *testing.T) {
		for acc := range app.GetMaccPerms() {
			addr := authtypes.NewModuleAddress(acc).String()
			if acc == govtypes.ModuleName {
				require.False(t, blocked[addr], "gov must remain able to receive funds")
				continue
			}
			require.True(t, blocked[addr], "module account %q must remain blocked", acc)
		}
	})
}
