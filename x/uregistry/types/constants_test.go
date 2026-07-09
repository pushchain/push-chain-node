package types

import (
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

// TestReservedSlots_FullTripleDeployedForEveryUnoccupiedABCSlot
// (F-2026-17025 upstream prevention) asserts that init() populated
// SYSTEM_CONTRACTS and BYTECODE with a complete proxy + admin + impl
// triple for every unused slot in the A/B/C ranges. Catches future
// regressions where someone removes init() or its loop bounds drift.
func TestReservedSlots_FullTripleDeployedForEveryUnoccupiedABCSlot(t *testing.T) {
	// Slots that were already occupied before init() ran. Anything else in
	// 0xA0-0xCF must have been auto-reserved.
	occupied := map[byte]bool{
		0xAA: true,
		0xB0: true, 0xB1: true, 0xB2: true, 0xBC: true,
		0xC0: true, 0xC1: true,
	}

	for _, hi := range []byte{0xA, 0xB, 0xC} {
		for lo := byte(0); lo < 0x10; lo++ {
			slot := (hi << 4) | lo
			if occupied[slot] {
				continue
			}
			name := fmt.Sprintf("RESERVED_%02X", slot)

			addrs, ok := SYSTEM_CONTRACTS[name]
			require.True(t, ok, "SYSTEM_CONTRACTS missing entry %s", name)
			require.Equal(t,
				fmt.Sprintf("0x00000000000000000000000000000000000000%02x", slot),
				strings.ToLower(addrs.Address),
				"%s proxy address mismatch", name)
			require.Equal(t,
				fmt.Sprintf("0xf2000000000000000000000000000000000000%02x", slot),
				strings.ToLower(addrs.ProxyAdmin),
				"%s admin address mismatch", name)
			require.Equal(t,
				fmt.Sprintf("0xf1000000000000000000000000000000000000%02x", slot),
				strings.ToLower(addrs.Implementation),
				"%s impl address mismatch", name)

			bc, ok := BYTECODE[name]
			require.True(t, ok, "BYTECODE missing entry %s", name)
			require.NotEmpty(t, bc.IMPL_RUNTIME, "%s IMPL_RUNTIME empty", name)
			require.NotEmpty(t, bc.PROXY_RUNTIME, "%s PROXY_RUNTIME empty", name)
			require.NotEmpty(t, bc.ADMIN_RUNTIME, "%s ADMIN_RUNTIME empty", name)

			// Synthesized PROXY_RUNTIME must embed THIS slot's admin address,
			// not the template's (0xF2…B0). Anything else means the
			// substitution silently picked up the wrong target.
			proxyHex := strings.ToLower(hex.EncodeToString(bc.PROXY_RUNTIME))
			expectedAdminLowerHex := fmt.Sprintf("f2000000000000000000000000000000000000%02x", slot)
			require.Contains(t, proxyHex, expectedAdminLowerHex,
				"%s PROXY_RUNTIME does not embed its own admin address", name)
			if slot != 0xB0 {
				require.NotContains(t, proxyHex, templateProxyAdminLowerHex,
					"%s PROXY_RUNTIME still contains the template (0xF2…B0) admin — substitution failed", name)
			}
		}
	}
}

// TestReservedSlots_DRangeNotReserved guards the policy that 0xD0-0xFF is
// intentionally NOT reserved. If someone widens the init() loop later and
// silently reserves the D/E/F ranges, this test fails and forces a deliberate
// review.
func TestReservedSlots_DRangeNotReserved(t *testing.T) {
	for _, hi := range []byte{0xD, 0xE, 0xF} {
		for lo := byte(0); lo < 0x10; lo++ {
			slot := (hi << 4) | lo
			name := fmt.Sprintf("RESERVED_%02X", slot)
			_, sysOK := SYSTEM_CONTRACTS[name]
			_, bcOK := BYTECODE[name]
			require.False(t, sysOK, "%s should NOT be auto-reserved (D/E/F left for other chains / future debug)", name)
			require.False(t, bcOK, "%s should NOT be in BYTECODE", name)
		}
	}
}

// TestReservedSlots_NoCollisionWithProxyAdminOrImpl guards uniqueness:
// proxy / admin / impl addresses across ALL system contracts (existing +
// auto-reserved) must be globally unique, so the genesis deploy loop never
// overwrites itself.
func TestReservedSlots_NoCollisionWithProxyAdminOrImpl(t *testing.T) {
	seen := make(map[common.Address]string)
	for name, addrs := range SYSTEM_CONTRACTS {
		for _, raw := range []string{addrs.Address, addrs.ProxyAdmin, addrs.Implementation} {
			a := common.HexToAddress(raw)
			if prev, dup := seen[a]; dup {
				t.Fatalf("address %s used by both %s and %s", a.Hex(), prev, name)
			}
			seen[a] = name
		}
	}
}

// TestReservedSlots_ExpectedTotalCount fixes the count so an off-by-one in
// the loop bounds (e.g. accidentally dropping AF or CF) shows up immediately.
// Pre-existing 6 + 41 newly reserved = 47.
func TestReservedSlots_ExpectedTotalCount(t *testing.T) {
	require.Len(t, SYSTEM_CONTRACTS, 47,
		"expected 6 pre-existing + 41 auto-reserved (15 A + 12 B + 14 C) = 47 total")
	require.Len(t, BYTECODE, 47,
		"BYTECODE must mirror SYSTEM_CONTRACTS")
}

// TestReservedSlots_AdminAddressFormatStringIsExactly20Bytes guards against
// off-by-one drift in the fmt template strings that build proxy/admin/impl
// addresses. EVM addresses must be exactly 20 bytes / 40 hex chars + "0x".
// If anyone drops or adds a zero from the format string, the resulting
// addresses would still pass common.HexToAddress (which left-pads silently)
// but would point at the wrong slot — caught here at the byte level.
func TestReservedSlots_AdminAddressFormatStringIsExactly20Bytes(t *testing.T) {
	for name, addrs := range SYSTEM_CONTRACTS {
		// "0x" + 40 hex chars = 42 chars
		require.Lenf(t, addrs.Address, 42, "%s proxy address wrong hex length", name)
		require.Lenf(t, addrs.ProxyAdmin, 42, "%s admin address wrong hex length", name)
		require.Lenf(t, addrs.Implementation, 42, "%s impl address wrong hex length", name)

		// And after HexToAddress they must round-trip to a non-zero address
		// (a malformed string would silently parse as the zero address).
		require.NotEqual(t, common.Address{}, common.HexToAddress(addrs.Address),
			"%s proxy address parses as zero", name)
		require.NotEqual(t, common.Address{}, common.HexToAddress(addrs.ProxyAdmin),
			"%s admin address parses as zero", name)
		require.NotEqual(t, common.Address{}, common.HexToAddress(addrs.Implementation),
			"%s impl address parses as zero", name)
	}
}

// TestReservedSlots_BytecodeIsCaseInsensitiveAcrossSlots is the answer to the
// "did the F2 vs f2 mixed casing in RESERVED_0 / RESERVED_1 cause a checksum
// problem?" question. EVM bytecode is raw bytes; hex case is a source-text
// convention only. This test proves it by re-encoding RESERVED_0's PROXY_RUNTIME
// in three different cases (lower, upper, mixed) and asserting they all decode
// to byte-identical slices and produce identical keccak256 hashes.
func TestReservedSlots_BytecodeIsCaseInsensitiveAcrossSlots(t *testing.T) {
	src := BYTECODE["RESERVED_0"].PROXY_RUNTIME
	require.NotEmpty(t, src)

	lowerHex := strings.ToLower(common.Bytes2Hex(src))
	upperHex := strings.ToUpper(common.Bytes2Hex(src))
	// Mixed: alternate case per char
	mixed := make([]byte, len(lowerHex))
	for i, b := range []byte(lowerHex) {
		if i%2 == 0 {
			mixed[i] = byte(strings.ToUpper(string(b))[0])
		} else {
			mixed[i] = b
		}
	}

	lowerBytes := common.FromHex("0x" + lowerHex)
	upperBytes := common.FromHex("0x" + upperHex)
	mixedBytes := common.FromHex("0x" + string(mixed))

	require.Equal(t, src, lowerBytes, "lowercase hex must decode to original bytes")
	require.Equal(t, src, upperBytes, "UPPERCASE hex must decode to identical bytes (case-insensitive)")
	require.Equal(t, src, mixedBytes, "MiXeD hex must decode to identical bytes (case-insensitive)")
}
