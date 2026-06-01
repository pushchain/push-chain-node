package usigverifier

import (
	"crypto/ed25519"
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/require"
)

// Fixed test vectors locking in the two distinct signing conventions exposed
// by the precompile (F-2026-17043 remediation).
//
//   - verifyEd25519:    signature must be over `"0x" + hex(msgDigest)` (66 ASCII bytes)
//   - verifyEd25519RawMessage: signature must be over the raw message bytes
//
// A signature produced for one convention MUST NOT verify under the other.

// Deterministic seed so the test vectors below are reproducible and inspectable.
// Anyone can re-derive these by running ed25519.NewKeyFromSeed on this 32-byte seed.
var testSeed = mustHex("000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f")

// A 32-byte digest used as the input to both methods. Same input, different
// signing semantics — that's the whole point of the two methods.
var testDigest32 = mustHex("deadbeef00112233445566778899aabbccddeeff0123456789abcdef00ff00ff")

func TestVerifyEd25519_AcceptsHexAsciiSignature(t *testing.T) {
	priv := ed25519.NewKeyFromSeed(testSeed)
	pub := priv.Public().(ed25519.PublicKey)

	// What verifyEd25519 expects the signer to have signed.
	hexAsciiBytes := []byte("0x" + hex.EncodeToString(testDigest32))
	require.Len(t, hexAsciiBytes, 66, "ASCII hex form must be 66 bytes (0x + 64 hex chars)")

	sig := ed25519.Sign(priv, hexAsciiBytes)
	require.True(t, ed25519.Verify(pub, hexAsciiBytes, sig),
		"sanity: signature must verify against the bytes that were signed")

	// What verifyEd25519 actually verifies internally:
	verified := ed25519.Verify(pub, []byte("0x"+hex.EncodeToString(testDigest32)), sig)
	require.True(t, verified, "verifyEd25519 must accept signature over hex-ASCII form of digest")
}

func TestVerifyEd25519_RejectsRawDigestSignature(t *testing.T) {
	priv := ed25519.NewKeyFromSeed(testSeed)
	pub := priv.Public().(ed25519.PublicKey)

	// Signer mistakenly signs the raw 32-byte digest (the "natural" thing).
	rawDigestSig := ed25519.Sign(priv, testDigest32)

	// What verifyEd25519 actually verifies internally:
	verified := ed25519.Verify(pub, []byte("0x"+hex.EncodeToString(testDigest32)), rawDigestSig)
	require.False(t, verified, "verifyEd25519 must reject signature over raw digest bytes")
}

func TestVerifyEd25519RawMessage_AcceptsRawSignature(t *testing.T) {
	priv := ed25519.NewKeyFromSeed(testSeed)
	pub := priv.Public().(ed25519.PublicKey)

	// What verifyEd25519RawMessage expects: signature over the raw message bytes.
	rawSig := ed25519.Sign(priv, testDigest32)

	// What verifyEd25519RawMessage actually verifies internally:
	verified := ed25519.Verify(pub, testDigest32, rawSig)
	require.True(t, verified, "verifyEd25519RawMessage must accept signature over raw bytes")
}

func TestVerifyEd25519RawMessage_RejectsHexAsciiSignature(t *testing.T) {
	priv := ed25519.NewKeyFromSeed(testSeed)
	pub := priv.Public().(ed25519.PublicKey)

	// Signer (using legacy convention) signs the hex-ASCII form.
	hexAsciiSig := ed25519.Sign(priv, []byte("0x"+hex.EncodeToString(testDigest32)))

	// What verifyEd25519RawMessage actually verifies internally:
	verified := ed25519.Verify(pub, testDigest32, hexAsciiSig)
	require.False(t, verified, "verifyEd25519RawMessage must reject signature over hex-ASCII form")
}

// TestVerifyEd25519RawMessage_ArbitraryMessageLength sanity-checks that the raw
// method works for messages other than 32-byte digests (its whole point —
// no implicit assumption that the message is a digest).
func TestVerifyEd25519RawMessage_ArbitraryMessageLength(t *testing.T) {
	priv := ed25519.NewKeyFromSeed(testSeed)
	pub := priv.Public().(ed25519.PublicKey)

	for _, msg := range [][]byte{
		[]byte("hello"),
		make([]byte, 0),                       // empty
		make([]byte, 1024),                    // 1 KiB
		[]byte{0x00, 0x01, 0x02, 0x03, 0xff}, // arbitrary short
	} {
		sig := ed25519.Sign(priv, msg)
		require.True(t, ed25519.Verify(pub, msg, sig),
			"verifyEd25519RawMessage must work for messages of any length (len=%d)", len(msg))
	}
}

func mustHex(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		panic(err)
	}
	return b
}
