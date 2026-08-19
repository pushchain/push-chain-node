package dkls

import (
	"crypto/sha256"
	"encoding/binary"
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

// Setup blobs are a tag-length-value list after a fixed header. The wrapper
// exposes decoders for the key ID, message and party names but not the
// threshold, so that one is read here. Values are laid out as:
//
//	tag    uint16 little endian
//	length uint16 little endian, stored as length-1
//	value  length bytes
//
// The header is MESSAGE_ID_SIZE(32) + 2 + 2. This mirrors the library's internal
// encoding, so TestSetupThreshold pins it: if the format changes, that test
// fails rather than this silently reading the wrong byte.
const (
	setupHeaderSize   = 36
	setupTagThreshold = 1
)

// SetupThreshold returns the threshold embedded in a keygen, keyrefresh or
// quorumchange setup blob. Sign setups carry no threshold and return an error.
func SetupThreshold(setupData []byte) (int, error) {
	if len(setupData) < setupHeaderSize {
		return 0, fmt.Errorf("setup message too short to contain a threshold")
	}
	for offset := setupHeaderSize; offset+4 <= len(setupData); {
		tag := binary.LittleEndian.Uint16(setupData[offset : offset+2])
		length := int(binary.LittleEndian.Uint16(setupData[offset+2:offset+4])) + 1
		valueStart := offset + 4
		if valueStart+length > len(setupData) {
			return 0, fmt.Errorf("setup message is malformed: tag %d claims %d bytes past the end", tag, length)
		}
		if tag == setupTagThreshold {
			// keygen and quorumchange store the threshold as a u8; the weighted
			// keygen variant uses a u16 under the same tag. Accept either so a
			// library upgrade widening it does not reject every setup.
			switch length {
			case 1:
				return int(setupData[valueStart]), nil
			case 2:
				return int(binary.LittleEndian.Uint16(setupData[valueStart : valueStart+2])), nil
			default:
				return 0, fmt.Errorf("threshold tag has unexpected length %d", length)
			}
		}
		offset = valueStart + length
	}
	return 0, fmt.Errorf("setup message carries no threshold")
}
