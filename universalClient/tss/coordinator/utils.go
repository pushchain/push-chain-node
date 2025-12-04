package coordinator

import (
	"crypto/sha256"
	"math/rand"

	"github.com/pushchain/push-chain-node/x/uvalidator/types"
)

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

// selectRandomThreshold selects a random subset of at least threshold count from eligible validators.
// Returns a shuffled copy of at least threshold validators (or all if fewer than threshold).
func selectRandomThreshold(eligible []*types.UniversalValidator) []*types.UniversalValidator {
	if len(eligible) == 0 {
		return nil
	}

	// Calculate minimum required: >2/3 (same as threshold calculation)
	minRequired := CalculateThreshold(len(eligible))

	// If we have fewer than minRequired, return all
	if len(eligible) <= minRequired {
		return eligible
	}

	// Randomly select at least minRequired participants
	// Shuffle and take first minRequired
	shuffled := make([]*types.UniversalValidator, len(eligible))
	copy(shuffled, eligible)
	rand.Shuffle(len(shuffled), func(i, j int) {
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	})

	return shuffled[:minRequired]
}
