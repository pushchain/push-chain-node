package dkls

import (
	"fmt"

	session "go-wrapper/go-dkls/sessions"
)

// keygenSession implements Session.
type keygenSession struct {
	sessionID    string
	partyID      string
	handle       session.Handle
	payloadCh    chan []byte
	participants []string // Party IDs in order
	sessionType  SessionType
}

// NewKeygenSession creates a new keygen session.
// setupData: The setup message (required - must be provided by caller)
// sessionID: Session identifier (typically eventID)
// partyID: This node's party ID
// participants: List of participant party IDs (sorted)
// threshold: The threshold for the keygen (number of parties needed to sign)
func NewKeygenSession(
	setupData []byte,
	sessionID string,
	partyID string,
	participants []string,
	threshold int,
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

	// Create session from setup
	handle, err := session.DklsKeygenSessionFromSetup(setupData, []byte(partyID))
	if err != nil {
		return nil, fmt.Errorf("failed to create keygen session: %w", err)
	}

	return &keygenSession{
		sessionID:    sessionID,
		partyID:      partyID,
		handle:       handle,
		payloadCh:    make(chan []byte, 256),
		participants: participants,
		sessionType:  SessionTypeKeygen,
	}, nil
}

// Step processes the next protocol step and returns messages to send.
// It processes any queued input messages first, then gets output messages.
// Returns: (messages to send, finished, error)
func (s *keygenSession) Step() ([]Message, bool, error) {
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

	// Get output messages from DKLS session
	var messages []Message
	for {
		msgData, err := session.DklsKeygenSessionOutputMessage(s.handle)
		if err != nil {
			return nil, false, fmt.Errorf("failed to get output message: %w", err)
		}
		if len(msgData) == 0 {
			break
		}

		// For each participant, check if this message is for them
		for idx := 0; idx < len(s.participants); idx++ {
			receiver, err := session.DklsKeygenSessionMessageReceiver(s.handle, msgData, idx)
			if err != nil {
				return nil, false, fmt.Errorf("failed to get message receiver: %w", err)
			}
			if receiver == "" {
				break
			}

			// If receiver is self, queue locally for next step
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
func (s *keygenSession) enqueuePayload(data []byte) error {
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
func (s *keygenSession) InputMessage(data []byte) error {
	return s.enqueuePayload(data)
}

// GetParticipants returns the list of participant party IDs.
func (s *keygenSession) GetParticipants() []string {
	// Return a copy to avoid mutation
	participants := make([]string, len(s.participants))
	copy(participants, s.participants)
	return participants
}

// GetType returns the type of the session.
func (s *keygenSession) GetType() SessionType {
	return s.sessionType
}

// GetResult returns the result when finished.
func (s *keygenSession) GetResult() (*Result, error) {
	// Finish the session
	keyHandle, err := session.DklsKeygenSessionFinish(s.handle)
	if err != nil {
		return nil, fmt.Errorf("failed to finish keygen session: %w", err)
	}
	defer session.DklsKeyshareFree(keyHandle)

	// Extract keyshare
	keyshare, err := session.DklsKeyshareToBytes(keyHandle)
	if err != nil {
		return nil, fmt.Errorf("failed to extract keyshare: %w", err)
	}

	// Extract keyID and publicKey from keyshare handle
	keyIDBytes, err := session.DklsKeyshareKeyID(keyHandle)
	if err != nil {
		return nil, fmt.Errorf("failed to extract keyID: %w", err)
	}
	keyID := string(keyIDBytes)

	publicKey, err := session.DklsKeysharePublicKey(keyHandle)
	if err != nil {
		return nil, fmt.Errorf("failed to extract publicKey: %w", err)
	}

	// Return participants list (copy to avoid mutation)
	participants := make([]string, len(s.participants))
	copy(participants, s.participants)

	return &Result{
		Keyshare:     keyshare,
		Signature:    nil,
		KeyID:        keyID,
		PublicKey:    publicKey,
		Participants: participants,
	}, nil
}

// Close cleans up the session.
func (s *keygenSession) Close() {
	if s.handle != 0 {
		session.DklsKeygenSessionFree(s.handle)
		s.handle = 0
	}
}
