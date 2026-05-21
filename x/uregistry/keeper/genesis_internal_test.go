package keeper

import (
	"fmt"
	"math/big"
	"sort"
	"strings"
	"testing"

	"cosmossdk.io/log"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/evm/x/vm/statedb"
	evmtypes "github.com/cosmos/evm/x/vm/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/pushchain/push-chain-node/x/uregistry/types"
	"github.com/stretchr/testify/require"
)

// stubEVMKeeper is a minimal EVMKeeper for testing isContractDeployed.
// Only GetAccount is exercised; the other interface methods panic if hit.
type stubEVMKeeper struct {
	accounts map[common.Address]*statedb.Account
}

func (s stubEVMKeeper) GetAccount(_ sdk.Context, addr common.Address) *statedb.Account {
	return s.accounts[addr]
}

func (stubEVMKeeper) SetAccount(_ sdk.Context, _ common.Address, _ statedb.Account) error {
	panic("not used in test")
}
func (stubEVMKeeper) SetState(_ sdk.Context, _ common.Address, _ common.Hash, _ []byte) {
	panic("not used in test")
}
func (stubEVMKeeper) GetCode(_ sdk.Context, _ common.Hash) []byte { panic("not used in test") }
func (stubEVMKeeper) SetCode(_ sdk.Context, _, _ []byte)          { panic("not used in test") }

// TestIsContractDeployed_RejectsEOAsAndAcceptsRealContracts (F-2026-17025)
// covers the cases the original length-only predicate got wrong. Specifically:
//
//   - A pre-existing EOA at a reserved system-contract address must NOT be
//     mistaken for a deployed contract — its CodeHash is the keccak256-of-empty
//     sentinel (32 bytes, non-empty), so the old `len(CodeHash) != 0` check
//     would have wrongly skipped deployment.
//   - A nil/zero CodeHash must NOT be treated as deployed.
//   - Only an account carrying actual contract-code hash (anything other than
//     the EmptyCodeHash sentinel) is "deployed".
func TestIsContractDeployed_RejectsEOAsAndAcceptsRealContracts(t *testing.T) {
	addrA := common.HexToAddress("0x00000000000000000000000000000000000000C0")
	addrB := common.HexToAddress("0x00000000000000000000000000000000000000Bc")
	addrC := common.HexToAddress("0x00000000000000000000000000000000000000C1")
	addrD := common.HexToAddress("0x00000000000000000000000000000000000000B0")
	addrMissing := common.HexToAddress("0x00000000000000000000000000000000000000B1")

	realCodeHash := common.HexToHash("0xdeadbeef00112233445566778899aabbccddeeff0123456789abcdef00ff00ff")

	stub := stubEVMKeeper{accounts: map[common.Address]*statedb.Account{
		// Touched EOA: balance present, CodeHash is the EmptyCodeHash sentinel.
		// This is the case the original predicate failed on.
		addrA: {
			Nonce:    0,
			Balance:  big.NewInt(1_000_000_000_000_000_000), // 1 ETH-equivalent
			CodeHash: evmtypes.EmptyCodeHash,
		},
		// Untouched-style account with explicit nil CodeHash. Not a contract.
		addrB: {
			Nonce:    0,
			Balance:  big.NewInt(0),
			CodeHash: nil,
		},
		// Account with empty (zero-length) CodeHash. Not a contract.
		addrC: {
			Nonce:    0,
			Balance:  big.NewInt(0),
			CodeHash: []byte{},
		},
		// Real contract: CodeHash points to actual code.
		addrD: {
			Nonce:    1,
			Balance:  big.NewInt(0),
			CodeHash: realCodeHash.Bytes(),
		},
		// addrMissing intentionally omitted from the map → GetAccount returns nil
	}}

	ctx := sdk.Context{}

	require.False(t, isContractDeployed(ctx, stub, addrA),
		"touched EOA with EmptyCodeHash sentinel must NOT be treated as deployed (F-2026-17025)")
	require.False(t, isContractDeployed(ctx, stub, addrB),
		"account with nil CodeHash must NOT be treated as deployed")
	require.False(t, isContractDeployed(ctx, stub, addrC),
		"account with zero-length CodeHash must NOT be treated as deployed")
	require.False(t, isContractDeployed(ctx, stub, addrMissing),
		"absent account (GetAccount returns nil) must NOT be treated as deployed")
	require.True(t, isContractDeployed(ctx, stub, addrD),
		"account with real (non-empty, non-sentinel) CodeHash MUST be treated as deployed")
}

// trackerEVMKeeper records every account/code/state write so the deployment
// test can assert which addresses got the triple.
type trackerEVMKeeper struct {
	accounts map[common.Address]statedb.Account
	code     map[string][]byte                       // hex(codeHash) -> bytecode
	state    map[common.Address]map[common.Hash]common.Hash
}

func newTrackerEVMKeeper() *trackerEVMKeeper {
	return &trackerEVMKeeper{
		accounts: make(map[common.Address]statedb.Account),
		code:     make(map[string][]byte),
		state:    make(map[common.Address]map[common.Hash]common.Hash),
	}
}

func (t *trackerEVMKeeper) GetAccount(_ sdk.Context, addr common.Address) *statedb.Account {
	if acc, ok := t.accounts[addr]; ok {
		return &acc
	}
	return nil
}

func (t *trackerEVMKeeper) SetAccount(_ sdk.Context, addr common.Address, account statedb.Account) error {
	t.accounts[addr] = account
	return nil
}

func (t *trackerEVMKeeper) SetState(_ sdk.Context, addr common.Address, key common.Hash, value []byte) {
	if t.state[addr] == nil {
		t.state[addr] = make(map[common.Hash]common.Hash)
	}
	t.state[addr][key] = common.BytesToHash(value)
}

func (t *trackerEVMKeeper) GetCode(_ sdk.Context, codeHash common.Hash) []byte {
	return t.code[codeHash.Hex()]
}

func (t *trackerEVMKeeper) SetCode(_ sdk.Context, codeHash, code []byte) {
	t.code[common.BytesToHash(codeHash).Hex()] = code
}

// TestDeploySystemContracts_DeploysFullTripleForEveryReservedAddress
// (F-2026-17025 upstream prevention) proves the genesis deploy loop actually
// installs proxy + admin + impl bytecode at every reserved address — not just
// that the SYSTEM_CONTRACTS map is populated. For each entry it verifies:
//
//  1. The proxy address has non-empty CodeHash (claims the slot vs EOA squatting).
//  2. The ProxyAdmin address has non-empty CodeHash.
//  3. The implementation address has non-empty CodeHash.
//  4. The ProxyAdmin's storage slot 0 (Ownable.owner) is set to
//     PROXY_ADMIN_OWNER_ADDRESS_HEX (the F-2026-16998 EOA owner — same for all
//     46 ProxyAdmins). This is the load-bearing assertion for the
//     "single owner controls every system-contract upgrade" trust assumption.
//  5. The proxy's EIP-1967 admin slot points to the right ProxyAdmin
//     (PROXY_ADMIN_SLOT) and impl slot points to the right implementation
//     (PROXY_IMPLEMENTATION_SLOT).
func TestDeploySystemContracts_DeploysFullTripleForEveryReservedAddress(t *testing.T) {
	tracker := newTrackerEVMKeeper()
	ctx := sdk.NewContext(nil, cmtproto.Header{}, false, log.NewNopLogger())

	deploySystemContracts(ctx, tracker, types.SYSTEM_CONTRACTS)

	expectedOwner := common.HexToAddress(types.PROXY_ADMIN_OWNER_ADDRESS_HEX)

	// Sanity: must have processed all 46 entries (6 explicit + 40 auto-reserved).
	require.Len(t, types.SYSTEM_CONTRACTS, 47, "SYSTEM_CONTRACTS size drift")

	for name, addrs := range types.SYSTEM_CONTRACTS {
		proxy := common.HexToAddress(addrs.Address)
		admin := common.HexToAddress(addrs.ProxyAdmin)
		impl := common.HexToAddress(addrs.Implementation)

		// (1)-(3) every address in the triple has bytecode.
		for label, a := range map[string]common.Address{"proxy": proxy, "admin": admin, "impl": impl} {
			acc, ok := tracker.accounts[a]
			require.True(t, ok, "%s %s (%s) not deployed", name, label, a.Hex())
			require.NotEmpty(t, acc.CodeHash, "%s %s (%s) has empty CodeHash", name, label, a.Hex())
			require.NotEmpty(t, tracker.code[common.BytesToHash(acc.CodeHash).Hex()],
				"%s %s (%s) CodeHash references no bytecode", name, label, a.Hex())
		}

		// (4) ProxyAdmin owner slot = the hardcoded EOA (F-2026-16998).
		ownerSlot, ok := tracker.state[admin][common.Hash{}]
		require.True(t, ok, "%s ProxyAdmin owner slot was never written", name)
		require.Equal(t, expectedOwner, common.BytesToAddress(ownerSlot.Bytes()),
			"%s ProxyAdmin owner mismatch (single-EOA trust assumption broken)", name)

		// (5) Proxy's EIP-1967 admin slot = ProxyAdmin address.
		gotAdmin, ok := tracker.state[proxy][types.PROXY_ADMIN_SLOT]
		require.True(t, ok, "%s proxy EIP-1967 admin slot was never written", name)
		require.Equal(t, admin, common.BytesToAddress(gotAdmin.Bytes()),
			"%s proxy EIP-1967 admin slot mismatch", name)

		// (5) Proxy's EIP-1967 implementation slot = implementation address.
		gotImpl, ok := tracker.state[proxy][types.PROXY_IMPLEMENTATION_SLOT]
		require.True(t, ok, "%s proxy EIP-1967 impl slot was never written", name)
		require.Equal(t, impl, common.BytesToAddress(gotImpl.Bytes()),
			"%s proxy EIP-1967 impl slot mismatch", name)
	}
}

// TestDeploySystemContracts_ReportEveryProxyAdminOwner runs deployment and
// reads back the post-deploy state for every entry, printing a row per
// SYSTEM_CONTRACTS entry showing its proxy / admin / impl / actual-owner
// values. Two purposes:
//
//   - Acts as a deterministic auditable report (run with `go test -v` and
//     paste the output into the audit response or a runbook).
//   - Asserts every ProxyAdmin owner equals PROXY_ADMIN_OWNER_ADDRESS_HEX —
//     not via the test helper variable (which could itself be wrong) but by
//     comparing the byte representation of the stored owner against the raw
//     hex constant the code is supposed to write.
func TestDeploySystemContracts_ReportEveryProxyAdminOwner(t *testing.T) {
	tracker := newTrackerEVMKeeper()
	ctx := sdk.NewContext(nil, cmtproto.Header{}, false, log.NewNopLogger())
	deploySystemContracts(ctx, tracker, types.SYSTEM_CONTRACTS)

	// Sort names so the report is deterministic across runs.
	names := make([]string, 0, len(types.SYSTEM_CONTRACTS))
	for name := range types.SYSTEM_CONTRACTS {
		names = append(names, name)
	}
	sort.Strings(names)

	t.Logf("=== System-contract deployment report (%d entries) ===", len(names))
	t.Logf("%-18s %-44s %-44s %-44s %-44s",
		"NAME", "PROXY", "PROXY_ADMIN", "IMPLEMENTATION", "PROXY_ADMIN_OWNER")

	expectedOwner := common.HexToAddress(types.PROXY_ADMIN_OWNER_ADDRESS_HEX)
	for _, name := range names {
		addrs := types.SYSTEM_CONTRACTS[name]
		proxy := common.HexToAddress(addrs.Address)
		admin := common.HexToAddress(addrs.ProxyAdmin)
		impl := common.HexToAddress(addrs.Implementation)

		ownerSlot, ok := tracker.state[admin][common.Hash{}]
		require.True(t, ok, "%s: ProxyAdmin slot 0 (owner) was never written", name)
		actualOwner := common.BytesToAddress(ownerSlot.Bytes())

		t.Logf("%-18s %-44s %-44s %-44s %-44s",
			name, proxy.Hex(), admin.Hex(), impl.Hex(), actualOwner.Hex())

		require.Equal(t, expectedOwner, actualOwner,
			"%s: ProxyAdmin owner != PROXY_ADMIN_OWNER_ADDRESS_HEX (single-EOA trust assumption broken)", name)
	}

	// Belt-and-suspenders: also assert the constant itself didn't drift.
	require.Equal(t,
		strings.ToLower("0xa96CaA79eb2312DbEb0B8E93c1Ce84C98b67bF11"),
		strings.ToLower(types.PROXY_ADMIN_OWNER_ADDRESS_HEX),
		"PROXY_ADMIN_OWNER_ADDRESS_HEX constant changed — confirm intentional rotation")
}

// TestDeploySystemContracts_AllReservedSlotsInABCRangeAreCovered enumerates
// every slot in the A/B/C ranges explicitly and asserts a deployment landed at
// each (skipping only the slots intentionally left to non-uregistry owners or
// the precompile address). Catches off-by-one in the reservation loop or
// silent removal of a slot from the map.
func TestDeploySystemContracts_AllReservedSlotsInABCRangeAreCovered(t *testing.T) {
	tracker := newTrackerEVMKeeper()
	ctx := sdk.NewContext(nil, cmtproto.Header{}, false, log.NewNopLogger())
	deploySystemContracts(ctx, tracker, types.SYSTEM_CONTRACTS)

	// Slots in A/B/C that uregistry does NOT own:
	//   0xAA — uexecutor PROXY_ADMIN (deployed by uexecutor's own genesis)
	uregistryDoesNotOwn := map[byte]bool{0xAA: true}

	for _, hi := range []byte{0xA, 0xB, 0xC} {
		for lo := byte(0); lo < 0x10; lo++ {
			slot := (hi << 4) | lo
			if uregistryDoesNotOwn[slot] {
				continue
			}
			proxyAddr := common.HexToAddress(fmt.Sprintf("0x00000000000000000000000000000000000000%02x", slot))
			acc, ok := tracker.accounts[proxyAddr]
			require.True(t, ok,
				"slot 0x%02X (%s) was not deployed — reservation loop may have drifted", slot, proxyAddr.Hex())
			require.NotEmpty(t, acc.CodeHash,
				"slot 0x%02X (%s) deployed but with empty CodeHash", slot, proxyAddr.Hex())
		}
	}
}
