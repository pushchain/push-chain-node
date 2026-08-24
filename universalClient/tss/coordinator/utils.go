package coordinator

import (
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"
	"math/rand"
	"strings"

	"github.com/ethereum/go-ethereum/crypto/secp256k1"

	"github.com/pushchain/push-chain-node/x/uvalidator/types"
)

// verifyECDSASignature verifies a (r||s) secp256k1 signature against a 33-byte
// compressed pubkey (hex, optionally 0x-prefixed). Mirrors dkls.signSession's
// verify path: decompress → ecdsa.Verify with r,s as big.Ints. Recovery byte
// on a 65-byte sig is ignored.
func verifyECDSASignature(pubkeyHex string, hash, signature []byte) error {
	if len(hash) != 32 {
		return fmt.Errorf("hash must be 32 bytes, got %d", len(hash))
	}
	if len(signature) != 64 && len(signature) != 65 {
		return fmt.Errorf("signature must be 64 or 65 bytes, got %d", len(signature))
	}
	pubBytes, err := hex.DecodeString(strings.TrimPrefix(strings.TrimSpace(pubkeyHex), "0x"))
	if err != nil {
		return fmt.Errorf("decode pubkey: %w", err)
	}
	if len(pubBytes) != 33 {
		return fmt.Errorf("pubkey must be 33 bytes (compressed), got %d", len(pubBytes))
	}
	vkX, vkY := secp256k1.DecompressPubkey(pubBytes)
	if vkX == nil || vkY == nil {
		return fmt.Errorf("failed to decompress pubkey")
	}
	vk := ecdsa.PublicKey{Curve: secp256k1.S256(), X: vkX, Y: vkY}
	r := new(big.Int).SetBytes(signature[:32])
	s := new(big.Int).SetBytes(signature[32:64])
	if !ecdsa.Verify(&vk, hash, r, s) {
		return fmt.Errorf("ECDSA signature does not verify")
	}
	return nil
}

// CalculateThreshold calculates the threshold as > 2/3 of participants.
// Formula: threshold = floor((2 * n) / 3) + 1
// This ensures threshold > 2/3 * n
func CalculateThreshold(numParticipants int) int {
	if numParticipants <= 0 {
		return 1
	}
	threshold := (2*numParticipants)/3 + 1
	if threshold > numParticipants {
		threshold = numParticipants
	}
	return threshold
}

// deriveKeyIDBytes derives key ID bytes from a string key ID using SHA256.
func deriveKeyIDBytes(keyID string) []byte {
	sum := sha256.Sum256([]byte(keyID))
	return sum[:]
}

// selectRandomThreshold selects a random threshold count of eligible validators.
// Returns a shuffled copy of threshold validators (or all if fewer than threshold).
// The caller supplies the threshold: for fund migration it belongs to the old key,
// not to the set of validators still holding its shares.
func selectRandomThreshold(eligible []*types.UniversalValidator, threshold int) []*types.UniversalValidator {
	if len(eligible) == 0 || threshold <= 0 {
		return nil
	}

	// If we have fewer than threshold, return all
	if len(eligible) <= threshold {
		return eligible
	}

	shuffled := make([]*types.UniversalValidator, len(eligible))
	copy(shuffled, eligible)
	rand.Shuffle(len(shuffled), func(i, j int) {
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	})

	return shuffled[:threshold]
}
