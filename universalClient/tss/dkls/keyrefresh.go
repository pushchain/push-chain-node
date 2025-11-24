package dkls

import (
	"fmt"

	session "go-wrapper/go-dkls/sessions"
)

// keyrefreshSession implements Session.
type keyrefreshSession struct {
	sessionID    string
	partyID      string
	handle       session.Handle
	payloadCh    chan []byte
	participants []string
}

// NewKeyrefreshSession creates a new keyrefresh session.
// setupData: The setup message (required - must be provided by caller)
// sessionID: Session identifier (typically eventID)
// partyID: This node's party ID
// participants: List of participant party IDs (sorted)
// threshold: The threshold for the keyrefresh
// oldKeyshare: The existing keyshare to refresh (keyID is extracted from keyshare)
func NewKeyrefreshSession(
	setupData []byte,
	sessionID string,
	partyID string,
	participants []string,
	threshold int,
	oldKeyshare []byte,
) (Session, error) {
	if len(setupData) == 0 {
		return nil, fmt.Errorf("setupData is required")
	}
	if partyID == "" {
		return nil, fmt.Errorf("party ID required")
	}
	if len(participants) == 0 {
		return nil, fmt.Errorf("participants required")
	}
	if len(oldKeyshare) == 0 {
		return nil, fmt.Errorf("old keyshare required")
	}

	// Load old keyshare
	oldHandle, err := session.DklsKeyshareFromBytes(oldKeyshare)
	if err != nil {
		return nil, fmt.Errorf("failed to load old keyshare: %w", err)
	}
	defer session.DklsKeyshareFree(oldHandle)

	// Create session from setup
	handle, err := session.DklsKeyRefreshSessionFromSetup(setupData, []byte(partyID), oldHandle)
	if err != nil {
		return nil, fmt.Errorf("failed to create keyrefresh session: %w", err)
	}

	return &keyrefreshSession{
		sessionID:    sessionID,
		partyID:      partyID,
		handle:       handle,
		payloadCh:    make(chan []byte, 256),
		participants: participants,
	}, nil
}

// Step processes the next protocol step and returns messages to send.
func (s *keyrefreshSession) Step() ([]Message, bool, error) {
	// Process any queued input messages first
	select {
	case payload := <-s.payloadCh:
		finished, err := session.DklsKeygenSessionInputMessage(s.handle, payload)
		if err != nil {
			return nil, false, fmt.Errorf("failed to process input message: %w", err)
		}
		if finished {
			return nil, true, nil
		}
	default:
	}

	// Get output messages
	var messages []Message
	for {
		msgData, err := session.DklsKeygenSessionOutputMessage(s.handle)
		if err != nil {
			return nil, false, fmt.Errorf("failed to get output message: %w", err)
		}
		if len(msgData) == 0 {
			break
		}

		for idx := 0; idx < len(s.participants); idx++ {
			receiver, err := session.DklsKeygenSessionMessageReceiver(s.handle, msgData, idx)
			if err != nil {
				return nil, false, fmt.Errorf("failed to get message receiver: %w", err)
			}
			if receiver == "" {
				break
			}

			if receiver == s.partyID {
				if err := s.enqueuePayload(msgData); err != nil {
					return nil, false, fmt.Errorf("failed to queue local message: %w", err)
				}
				continue
			}

			messages = append(messages, Message{
				Receiver: receiver,
				Data:     msgData,
			})
		}
	}

	return messages, false, nil
}

// enqueuePayload queues a payload message for the session.
func (s *keyrefreshSession) enqueuePayload(data []byte) error {
	buf := make([]byte, len(data))
	copy(buf, data)
	select {
	case s.payloadCh <- buf:
		return nil
	default:
		return fmt.Errorf("payload buffer full for session %s", s.sessionID)
	}
}

// InputMessage processes an incoming protocol message.
func (s *keyrefreshSession) InputMessage(data []byte) error {
	return s.enqueuePayload(data)
}

// GetParticipants returns the list of participant party IDs.
func (s *keyrefreshSession) GetParticipants() []string {
	// Return a copy to avoid mutation
	participants := make([]string, len(s.participants))
	copy(participants, s.participants)
	return participants
}

// GetResult returns the result when finished.
func (s *keyrefreshSession) GetResult() (*Result, error) {
	keyHandle, err := session.DklsKeygenSessionFinish(s.handle)
	if err != nil {
		return nil, fmt.Errorf("failed to finish keyrefresh session: %w", err)
	}
	defer session.DklsKeyshareFree(keyHandle)

	keyshare, err := session.DklsKeyshareToBytes(keyHandle)
	if err != nil {
		return nil, fmt.Errorf("failed to extract keyshare: %w", err)
	}

	// Return participants list (copy to avoid mutation)
	participants := make([]string, len(s.participants))
	copy(participants, s.participants)

	return &Result{
		Keyshare:     keyshare,
		Signature:    nil,
		Participants: participants,
	}, nil
}

// Close cleans up the session.
func (s *keyrefreshSession) Close() {
	if s.handle != 0 {
		session.DklsKeygenSessionFree(s.handle)
		s.handle = 0
	}
}
