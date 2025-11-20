package node

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/libp2p/go-libp2p/core/crypto"

	"github.com/pushchain/push-chain-node/universalClient/tss"
)

// calculateThreshold calculates the threshold as > 2/3 of participants.
// Formula: threshold = floor((2 * n) / 3) + 1
// This ensures threshold > 2/3 * n
func calculateThreshold(numParticipants int) int {
	if numParticipants <= 0 {
		return 1
	}
	threshold := (2*numParticipants)/3 + 1
	if threshold > numParticipants {
		threshold = numParticipants
	}
	return threshold
}

// convertPrivateKeyHexToBase64 converts a hex-encoded Ed25519 private key to base64-encoded libp2p format.
func convertPrivateKeyHexToBase64(hexKey string) (string, error) {
	hexKey = strings.TrimSpace(hexKey)
	keyBytes, err := hex.DecodeString(hexKey)
	if err != nil {
		return "", fmt.Errorf("hex decode failed: %w", err)
	}
	if len(keyBytes) != 32 {
		return "", fmt.Errorf("wrong key length: got %d bytes, expected 32", len(keyBytes))
	}

	privKey := ed25519.NewKeyFromSeed(keyBytes)
	pubKey := privKey.Public().(ed25519.PublicKey)

	libp2pKeyBytes := make([]byte, 64)
	copy(libp2pKeyBytes[:32], privKey[:32])
	copy(libp2pKeyBytes[32:], pubKey)

	libp2pPrivKey, err := crypto.UnmarshalEd25519PrivateKey(libp2pKeyBytes)
	if err != nil {
		return "", fmt.Errorf("failed to unmarshal Ed25519 key: %w", err)
	}

	marshaled, err := crypto.MarshalPrivateKey(libp2pPrivKey)
	if err != nil {
		return "", fmt.Errorf("marshal failed: %w", err)
	}

	return base64.StdEncoding.EncodeToString(marshaled), nil
}

// isCoordinator determines if this node is the coordinator for the given block number.
func isCoordinator(blockNumber uint64, coordinatorRange uint64, validatorAddress string, participants []*tss.UniversalValidator) bool {
	if len(participants) == 0 {
		return false
	}
	epoch := blockNumber / coordinatorRange
	idx := int(epoch % uint64(len(participants)))
	if idx >= len(participants) {
		return false
	}
	return participants[idx].PartyID() == validatorAddress
}
