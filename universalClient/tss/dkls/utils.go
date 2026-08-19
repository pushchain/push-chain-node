package dkls

import (
	"crypto/sha256"
	"fmt"

	session "go-wrapper/go-dkls/sessions"
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

// SetupParticipants returns the participant list embedded in a DKLS setup blob,
// in index order. The setup is what actually drives the session, so callers must
// confirm it matches the participants they validated. Otherwise a coordinator
// can present one list for validation and run the session over another.
//
// Party names decode by index and come back empty past the end, which is how the
// list terminates.
func SetupParticipants(setupData []byte) ([]string, error) {
	if len(setupData) == 0 {
		return nil, fmt.Errorf("setupData is required")
	}
	var participants []string
	for i := 0; ; i++ {
		name, err := session.DklsDecodePartyName(setupData, i)
		if err != nil {
			return nil, fmt.Errorf("failed to decode party name at index %d: %w", i, err)
		}
		if len(name) == 0 {
			break
		}
		participants = append(participants, string(name))
	}
	return participants, nil
}
