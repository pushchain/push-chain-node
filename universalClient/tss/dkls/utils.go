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

// --- Setup decoding -------------------------------------------------------
// The coordinator supplies the setup blob and the values a follower validates
// separately. DKLS runs on the blob, so these expose what it actually contains
// and let callers bind the two. Both return an error on a malformed blob.

// SetupMessageHash returns the message hash embedded in a sign setup blob.
// DklsSignSessionFromSetup signs over the setup, not over any hash passed
// alongside it, so callers must confirm the two agree.
func SetupMessageHash(setupData []byte) ([]byte, error) {
	if len(setupData) == 0 {
		return nil, fmt.Errorf("setupData is required")
	}
	return session.DklsDecodeMessage(setupData)
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
