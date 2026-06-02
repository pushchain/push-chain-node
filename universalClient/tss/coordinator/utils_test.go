package coordinator

import (
	"crypto/ecdsa"
	"crypto/rand"
	"encoding/hex"
	"testing"

	"github.com/ethereum/go-ethereum/crypto/secp256k1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// compressPubkey returns the 33-byte compressed form of an ECDSA pubkey.
func compressPubkey(t *testing.T, pub *ecdsa.PublicKey) []byte {
	t.Helper()
	out := make([]byte, 33)
	if pub.Y.Bit(0) == 0 {
		out[0] = 0x02
	} else {
		out[0] = 0x03
	}
	xBytes := pub.X.Bytes()
	copy(out[33-len(xBytes):], xBytes)
	return out
}

func TestVerifyECDSASignature(t *testing.T) {
	// Build a valid (pubkey, hash, signature) triple once for the happy path
	// and to mutate for the failure cases.
	priv, err := ecdsa.GenerateKey(secp256k1.S256(), rand.Reader)
	require.NoError(t, err)
	pubHex := hex.EncodeToString(compressPubkey(t, &priv.PublicKey))

	hash := make([]byte, 32)
	for i := range hash {
		hash[i] = byte(i + 1)
	}
	r, s, err := ecdsa.Sign(rand.Reader, priv, hash)
	require.NoError(t, err)
	sig := make([]byte, 64)
	rBytes := r.Bytes()
	sBytes := s.Bytes()
	copy(sig[32-len(rBytes):32], rBytes)
	copy(sig[64-len(sBytes):64], sBytes)

	t.Run("valid signature verifies", func(t *testing.T) {
		assert.NoError(t, verifyECDSASignature(pubHex, hash, sig))
	})

	t.Run("valid signature with 0x-prefixed pubkey verifies", func(t *testing.T) {
		assert.NoError(t, verifyECDSASignature("0x"+pubHex, hash, sig))
	})

	t.Run("65-byte signature (recovery byte ignored)", func(t *testing.T) {
		sig65 := append(append([]byte{}, sig...), 0x00)
		assert.NoError(t, verifyECDSASignature(pubHex, hash, sig65))
	})

	t.Run("hash mismatch fails", func(t *testing.T) {
		bad := make([]byte, 32)
		bad[0] = 0xff
		err := verifyECDSASignature(pubHex, bad, sig)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "does not verify")
	})

	t.Run("hash wrong length", func(t *testing.T) {
		err := verifyECDSASignature(pubHex, make([]byte, 31), sig)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "hash must be 32 bytes")
	})

	t.Run("signature wrong length", func(t *testing.T) {
		err := verifyECDSASignature(pubHex, hash, make([]byte, 63))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must be 64 or 65 bytes")
	})

	t.Run("pubkey not hex", func(t *testing.T) {
		err := verifyECDSASignature("not-hex", hash, sig)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "decode pubkey")
	})

	t.Run("pubkey wrong length", func(t *testing.T) {
		err := verifyECDSASignature(hex.EncodeToString(make([]byte, 32)), hash, sig)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "33 bytes")
	})

	t.Run("signature for different key fails", func(t *testing.T) {
		other, err := ecdsa.GenerateKey(secp256k1.S256(), rand.Reader)
		require.NoError(t, err)
		otherHex := hex.EncodeToString(compressPubkey(t, &other.PublicKey))
		err = verifyECDSASignature(otherHex, hash, sig)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "does not verify")
	})

	// Tamper with one byte of r — verifier must reject.
	t.Run("tampered signature fails", func(t *testing.T) {
		tampered := append([]byte{}, sig...)
		tampered[0] ^= 0xff
		err := verifyECDSASignature(pubHex, hash, tampered)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "does not verify")
	})

	// Sanity check: make sure compressPubkey matches what secp256k1 produces.
	t.Run("compressPubkey round-trips", func(t *testing.T) {
		compressed := compressPubkey(t, &priv.PublicKey)
		xRoundtrip, yRoundtrip := secp256k1.DecompressPubkey(compressed)
		require.NotNil(t, xRoundtrip)
		assert.Equal(t, 0, priv.PublicKey.X.Cmp(xRoundtrip))
		assert.Equal(t, 0, priv.PublicKey.Y.Cmp(yRoundtrip))
	})

}
