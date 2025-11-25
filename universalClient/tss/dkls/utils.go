package dkls

import (
	"crypto/sha256"
)

// deriveKeyID derives a key ID bytes from a string key ID.
func deriveKeyID(keyID string) []byte {
	sum := sha256.Sum256([]byte(keyID))
	return sum[:]
}

// encodeParticipantIDs encodes a list of participant party IDs into bytes.
// IDs are separated by null bytes.
func encodeParticipantIDs(participants []string) []byte {
	ids := make([]byte, 0, len(participants)*10)
	for i, partyID := range participants {
		if i > 0 {
			ids = append(ids, 0) // Separator
		}
		ids = append(ids, []byte(partyID)...)
	}
	return ids
}
