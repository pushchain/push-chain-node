package utils_test

import (
	"encoding/hex"
	"math/rand"
	"strings"
	"testing"
	"time"

	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/mr-tron/base58"
	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/utils"
)

const (
	eip55Addr = "0x5aAeb6053F3E94C9b9A09f33669435E7Ef1BeAed"
	lowerAddr = "0x5aaeb6053f3e94c9b9a09f33669435e7ef1beaed"
	upperAddr = "0X5AAEB6053F3E94C9B9A09F33669435E7EF1BEAED"
	noPfxAddr = "5aaeb6053f3e94c9b9a09f33669435e7ef1beaed"
	mixedHash = "0xB28F49668e7e76dc96D7aaBE5b7f63FEcfbd1c3574774c05e8204e749fd96fbd"
	lowerHash = "0xb28f49668e7e76dc96d7aabe5b7f63fecfbd1c3574774c05e8204e749fd96fbd"
	noPfxHash = "b28f49668e7e76dc96d7aabe5b7f63fecfbd1c3574774c05e8204e749fd96fbd"
	solPubkey = "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v"
	solSig    = "5j7s6NiJS3JAkvgkoc18WVAsiSaci2pxB2A6ueCJP4tprA2TFg9wSyTLeYouxPBJEMzJinENTkpA52YStRW5Dia7"
)

func TestCanonicalizeEVMAddress_EquivalentEncodingsConverge(t *testing.T) {
	for _, in := range []string{eip55Addr, lowerAddr, upperAddr, noPfxAddr, "  " + eip55Addr + "  "} {
		got, err := utils.CanonicalizeEVMAddress(in)
		require.NoError(t, err, "input %q", in)
		require.Equal(t, eip55Addr, got, "input %q must canonicalize to EIP-55", in)
	}
}

func TestCanonicalizeEVMAddress_RejectsMalformed(t *testing.T) {
	for _, in := range []string{"", "0x12", "0xZZaeb6053f3e94c9b9a09f33669435e7ef1beaed", lowerAddr + "ab", "not-an-address"} {
		_, err := utils.CanonicalizeEVMAddress(in)
		require.Error(t, err, "input %q must be rejected", in)
	}
}

func TestCanonicalizeEVMHash_EquivalentEncodingsConverge(t *testing.T) {
	upper0X := "0X" + "B28F49668E7E76DC96D7AABE5B7F63FECFBD1C3574774C05E8204E749FD96FBD"
	for _, in := range []string{mixedHash, lowerHash, noPfxHash, upper0X, " " + lowerHash + " "} {
		got, err := utils.CanonicalizeEVMHash(in)
		require.NoError(t, err, "input %q", in)
		require.Equal(t, lowerHash, got, "input %q must canonicalize to 0x-lowercase", in)
	}
}

func TestCanonicalizeEVMHash_Keeps0xPrefix(t *testing.T) {
	got, err := utils.CanonicalizeEVMHash(noPfxHash)
	require.NoError(t, err)
	require.Equal(t, "0x", got[:2], "canonical hash form must keep the 0x prefix")
}

func TestCanonicalizeEVMHash_RejectsMalformed(t *testing.T) {
	for _, in := range []string{"", "0x1234", lowerHash + "00", "0xZZ" + noPfxHash[2:]} {
		_, err := utils.CanonicalizeEVMHash(in)
		require.Error(t, err, "input %q must be rejected", in)
	}
}

func TestCanonicalizeAddressByNamespace_Solana_PreservesBase58Case(t *testing.T) {
	got, err := utils.CanonicalizeAddressByNamespace("solana:mainnet", solPubkey)
	require.NoError(t, err)
	require.Equal(t, solPubkey, got, "base58 pubkeys are case-significant and must not be altered")
}

func TestCanonicalizeAddressByNamespace_Solana_HexLowercased(t *testing.T) {
	got, err := utils.CanonicalizeAddressByNamespace("solana:devnet", "0xABCDEF12")
	require.NoError(t, err)
	require.Equal(t, "0xabcdef12", got)
}

func TestCanonicalizeAddressByNamespace_EVM(t *testing.T) {
	got, err := utils.CanonicalizeAddressByNamespace("eip155:1", lowerAddr)
	require.NoError(t, err)
	require.Equal(t, eip55Addr, got)
}

func TestCanonicalizeAddressByNamespace_EmptyPassthrough(t *testing.T) {
	got, err := utils.CanonicalizeAddressByNamespace("eip155:1", "")
	require.NoError(t, err)
	require.Equal(t, "", got)
}

func TestCanonicalizeAddressByNamespace_UnknownNamespaceTrims(t *testing.T) {
	got, err := utils.CanonicalizeAddressByNamespace("cosmos:push", "  push1abc  ")
	require.NoError(t, err)
	require.Equal(t, "push1abc", got)
}

func TestCanonicalizeTxHashByNamespace_EVM(t *testing.T) {
	got, err := utils.CanonicalizeTxHashByNamespace("eip155:11155111", mixedHash)
	require.NoError(t, err)
	require.Equal(t, lowerHash, got)
}

func TestCanonicalizeTxHashByNamespace_Solana_Base58SigConvergesWithHex(t *testing.T) {
	// The reference client converts base58 signatures to 0x-hex before
	// submitting; a client submitting raw base58 must land on the same form.
	fromB58, err := utils.CanonicalizeTxHashByNamespace("solana:devnet", solSig)
	require.NoError(t, err)
	require.Equal(t, "0x", fromB58[:2], "64-byte base58 signature should converge to 0x-hex")
	require.Len(t, fromB58, 2+128)

	again, err := utils.CanonicalizeTxHashByNamespace("solana:devnet", fromB58)
	require.NoError(t, err)
	require.Equal(t, fromB58, again, "canonicalization must be idempotent")
}

func TestCanonicalizeTxHashByNamespace_Solana_NonSigBase58Preserved(t *testing.T) {
	// 32-byte base58 values (pubkey-length) are not signatures; preserved as-is.
	got, err := utils.CanonicalizeTxHashByNamespace("solana:devnet", solPubkey)
	require.NoError(t, err)
	require.Equal(t, solPubkey, got)
}

func TestCanonicalizeTxHashByNamespace_EmptyPassthrough(t *testing.T) {
	got, err := utils.CanonicalizeTxHashByNamespace("eip155:1", "")
	require.NoError(t, err)
	require.Equal(t, "", got)
}

func TestCanonicalizeHexBlob(t *testing.T) {
	require.Equal(t, "0xabcd12", utils.CanonicalizeHexBlob("0xABCD12"))
	require.Equal(t, "0xabcd12", utils.CanonicalizeHexBlob("ABCD12"))
	require.Equal(t, "", utils.CanonicalizeHexBlob("  "))
	// Non-hex content is preserved trimmed, never mangled.
	require.Equal(t, "not-hex", utils.CanonicalizeHexBlob(" not-hex "))
	// Odd-length hex strings are not valid byte blobs; preserved.
	require.Equal(t, "0xabc", utils.CanonicalizeHexBlob("0xabc"))
}

func TestCAIP2Namespace(t *testing.T) {
	require.Equal(t, "eip155", utils.CAIP2Namespace("eip155:1"))
	require.Equal(t, "solana", utils.CAIP2Namespace("solana:EtWTRABZaYq6iMfeYKouRu166VU2xqa1"))
	require.Equal(t, "", utils.CAIP2Namespace("no-colon"))
}

// referenceSolanaTxHash reproduces the pre-fix behaviour for pure-base58 input:
// decode unconditionally, convert only on an exact 64-byte result, otherwise
// return the input untouched. The length band added in canonicalizeSolanaTxHash
// must not change the result for any input.
func referenceSolanaTxHash(s string) string {
	if raw, err := base58.Decode(s); err == nil && len(raw) == 64 {
		return "0x" + hex.EncodeToString(raw)
	}
	return s
}

func TestCanonicalizeTxHashByNamespace_Solana_LengthBandIsOutputEquivalent(t *testing.T) {
	// Only 64..88 base58 chars can decode to exactly 64 bytes, so the band gate
	// is a pure performance change. Sweep across it — 63/64/88/89 are the edges.
	rng := rand.New(rand.NewSource(1))
	alphabet := []byte("123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz")

	lengths := []int{1, 2, 31, 32, 43, 44, 63, 64, 65, 87, 88, 89, 90, 128, 200, 300}
	for n := 3; n < 63; n += 7 {
		lengths = append(lengths, n)
	}

	for _, n := range lengths {
		for variant := 0; variant < 4; variant++ {
			b := make([]byte, n)
			for i := range b {
				switch variant {
				case 0:
					b[i] = '1' // all-zero decode: the short edge of the band
				case 1:
					b[i] = 'z' // largest digit: the long edge
				default:
					b[i] = alphabet[rng.Intn(len(alphabet))]
				}
			}
			in := string(b)
			require.Equal(t, referenceSolanaTxHash(in),
				utils.LenientCanonicalizeTxHash("solana:devnet", in),
				"length band changed the result for a %d-char input %q", n, in)
		}
	}
}

func TestCanonicalizeTxHashByNamespace_Solana_RealSignatureStillConverges(t *testing.T) {
	// The band must not break the case it exists to serve: an 88-char base58
	// signature still folds to 0x-hex.
	got, err := utils.CanonicalizeTxHashByNamespace("solana:devnet", solSig)
	require.NoError(t, err)
	require.Equal(t, "0x", got[:2])
	require.Len(t, got, 2+128)
}

func TestCanonicalizeTxHashByNamespace_Solana_OversizedInputDoesNotDecode(t *testing.T) {
	// F-2026-18821: mr-tron/base58 decoding is quadratic, and the result for an
	// out-of-band length is discarded. Before the fix a single 1e5-char decode
	// measured 4.5-29s (and InboundKeys does three of them); after, no decode
	// runs at all. The bound is loose enough not to flake on a busy CI box while
	// still failing hard on any return to O(n^2).
	huge := strings.Repeat("z", 100_000)

	start := time.Now()
	got := utils.LenientCanonicalizeTxHash("solana:devnet", huge)
	elapsed := time.Since(start)

	require.Equal(t, huge, got, "out-of-band input must pass through unchanged")
	require.Less(t, elapsed, time.Second,
		"oversized base58 tx_hash must not be decoded (took %s)", elapsed)
}

// AddressToBytes32: an EVM address goes into the low 20 bytes (bytes32(uint160(addr))).
func TestAddressToBytes32_EVM_LowAligned(t *testing.T) {
	addr := "0x000000000000000000000000000000000000dEaD"
	h, err := utils.AddressToBytes32("eip155:11155111", addr)
	require.NoError(t, err)
	require.Equal(t, ethcommon.HexToAddress(addr).Bytes(), h.Bytes()[12:], "address occupies the low 20 bytes")
	require.Equal(t, make([]byte, 12), h.Bytes()[:12], "high 12 bytes are zero")

	// Case-insensitive input converges to the same key.
	h2, err := utils.AddressToBytes32("eip155:11155111", "0x000000000000000000000000000000000000dead")
	require.NoError(t, err)
	require.Equal(t, h, h2)
}

// AddressToBytes32: a Solana pubkey is the raw 32 bytes, and base58 vs 0x-hex of
// the same pubkey converge to the same key.
func TestAddressToBytes32_Solana(t *testing.T) {
	raw := make([]byte, 32)
	for i := range raw {
		raw[i] = byte(i + 1)
	}
	const solChain = "solana:5eykt4UsFv8P8NJdTREpY1vzqKqZKvdp"

	hB58, err := utils.AddressToBytes32(solChain, base58.Encode(raw))
	require.NoError(t, err)
	require.Equal(t, raw, hB58.Bytes(), "solana pubkey is the raw 32 bytes")

	hHex, err := utils.AddressToBytes32(solChain, "0x"+hex.EncodeToString(raw))
	require.NoError(t, err)
	require.Equal(t, hB58, hHex, "base58 and 0x-hex of the same pubkey converge")
}

// AddressToBytes32 validates the address against the chain and rejects mismatches.
func TestAddressToBytes32_Rejects(t *testing.T) {
	cases := []struct {
		name, chain, addr string
	}{
		{"base58 on an EVM chain", "eip155:1", "So11111111111111111111111111111111111111112"},
		{"20-byte EVM addr on a Solana chain", "solana:x", "0x000000000000000000000000000000000000dEaD"},
		{"malformed EVM (wrong length)", "eip155:1", "0x1234"},
		{"empty", "eip155:1", ""},
		{"unsupported namespace", "cosmos:1", "0x000000000000000000000000000000000000dEaD"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := utils.AddressToBytes32(tc.chain, tc.addr)
			require.Error(t, err)
		})
	}
}
