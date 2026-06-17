package utils_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/utils"
)

const (
	eip55Addr  = "0x5aAeb6053F3E94C9b9A09f33669435E7Ef1BeAed"
	lowerAddr  = "0x5aaeb6053f3e94c9b9a09f33669435e7ef1beaed"
	upperAddr  = "0X5AAEB6053F3E94C9B9A09F33669435E7EF1BEAED"
	noPfxAddr  = "5aaeb6053f3e94c9b9a09f33669435e7ef1beaed"
	mixedHash  = "0xB28F49668e7e76dc96D7aaBE5b7f63FEcfbd1c3574774c05e8204e749fd96fbd"
	lowerHash  = "0xb28f49668e7e76dc96d7aabe5b7f63fecfbd1c3574774c05e8204e749fd96fbd"
	noPfxHash  = "b28f49668e7e76dc96d7aabe5b7f63fecfbd1c3574774c05e8204e749fd96fbd"
	solPubkey  = "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v"
	solSig     = "5j7s6NiJS3JAkvgkoc18WVAsiSaci2pxB2A6ueCJP4tprA2TFg9wSyTLeYouxPBJEMzJinENTkpA52YStRW5Dia7"
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
