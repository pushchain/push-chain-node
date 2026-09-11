package integrationtest

import (
	"testing"

	evmtypes "github.com/cosmos/evm/x/vm/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	utils "github.com/pushchain/push-chain-node/test/utils"
	uregistrytypes "github.com/pushchain/push-chain-node/x/uregistry/types"
)

// The read-state upgrade must reserve every system-contract slot that is still
// empty, and leave the ones already deployed exactly as they are.
//
// UNIVERSAL_CALLBACK (0x…C2) was added to SYSTEM_CONTRACTS after donut launched, so
// on that chain its proxy, admin and implementation are all bare — verified against
// the live testnet. x/ucallback only accepts ReadRequested logs from that exact
// address, so if the upgrade does not reserve it the module is inert.
func TestReadStateUpgrade_ReservesMissingSystemContracts(t *testing.T) {
	chainApp, ctx, _ := utils.SetAppWithValidators(t)
	k := chainApp.UregistryKeeper

	code := func(addr string) []byte {
		a := common.HexToAddress(addr)
		acct := chainApp.EVMKeeper.GetAccountOrEmpty(ctx, a)
		return chainApp.EVMKeeper.GetCode(ctx, common.BytesToHash(acct.CodeHash))
	}

	// Simulate a chain that launched before the slot existed: clear the triple.
	cb := uregistrytypes.SYSTEM_CONTRACTS["UNIVERSAL_CALLBACK"]
	for _, a := range []string{cb.Address, cb.ProxyAdmin, cb.Implementation} {
		// Point the account back at the empty-code sentinel; that is exactly the
		// state the reservation guard treats as "not deployed".
		chainApp.EVMKeeper.SetCodeHash(ctx,
			common.HexToAddress(a).Bytes(), evmtypes.EmptyCodeHash)
	}
	require.Empty(t, code(cb.Address), "precondition: the slot must start bare")

	// A slot that IS deployed must be left untouched.
	core := uregistrytypes.SYSTEM_CONTRACTS["UNIVERSAL_CORE"]
	coreBefore := code(core.Address)
	require.NotEmpty(t, coreBefore, "precondition: UNIVERSAL_CORE is deployed at genesis")

	require.NoError(t, k.DeployMissingSystemContracts(ctx))

	require.NotEmpty(t, code(cb.Address), "the callback proxy must be reserved")
	require.NotEmpty(t, code(cb.ProxyAdmin), "its ProxyAdmin must be reserved")
	require.NotEmpty(t, code(cb.Implementation), "its implementation must be reserved")
	require.Equal(t, coreBefore, code(core.Address),
		"an already-deployed contract must not be redeployed or overwritten")
}

// Running the reservation twice must change nothing. The upgrade runs once, but the
// same guard protects chains where the slot is already present, and a second pass is
// the cheapest way to prove the guard actually holds.
func TestReadStateUpgrade_ReservationIsIdempotent(t *testing.T) {
	chainApp, ctx, _ := utils.SetAppWithValidators(t)
	k := chainApp.UregistryKeeper

	code := func(addr string) []byte {
		a := common.HexToAddress(addr)
		acct := chainApp.EVMKeeper.GetAccountOrEmpty(ctx, a)
		return chainApp.EVMKeeper.GetCode(ctx, common.BytesToHash(acct.CodeHash))
	}

	require.NoError(t, k.DeployMissingSystemContracts(ctx))

	before := map[string][]byte{}
	for name, c := range uregistrytypes.SYSTEM_CONTRACTS {
		before[name] = code(c.Address)
		require.NotEmpty(t, before[name], "%s must be reserved after the first pass", name)
	}

	require.NoError(t, k.DeployMissingSystemContracts(ctx))

	for name, c := range uregistrytypes.SYSTEM_CONTRACTS {
		require.Equal(t, before[name], code(c.Address), "%s changed on the second pass", name)
	}
}
