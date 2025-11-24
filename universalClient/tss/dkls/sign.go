package dkls

import (
	"fmt"

	session "go-wrapper/go-dkls/sessions"
)

// signSession implements Session.
type signSession struct {
	sessionID    string
	partyID      string
	handle       session.Handle
	payloadCh    chan []byte
	participants []string
}

// NewSignSession creates a new sign session.
// setupData: The setup message (must be provided - keyID is extracted from keyshare by caller)
// sessionID: Session identifier (typically eventID)
// partyID: This node's party ID
// participants: List of participant party IDs (sorted)
// keyshare: The keyshare to use for signing (keyID is extracted from keyshare)
// messageHash: The message hash to sign
// chainPath: Optional chain path (nil if empty)
func NewSignSession(
	setupData []byte,
	sessionID string,
	partyID string,
	participants []string,
	keyshare []byte,
	messageHash []byte,
	chainPath []byte,
) (Session, error) {
	// setupData is required - check first
	if len(setupData) == 0 {
		return nil, fmt.Errorf("setupData is required")
	}
	if partyID == "" {
		return nil, fmt.Errorf("party ID required")
	}
	if len(participants) == 0 {
		return nil, fmt.Errorf("participants required")
	}
	if len(keyshare) == 0 {
		return nil, fmt.Errorf("keyshare required")
	}
	if len(messageHash) == 0 {
		return nil, fmt.Errorf("message hash required")
	}

	// Load keyshare
	keyshareHandle, err := session.DklsKeyshareFromBytes(keyshare)
	if err != nil {
		return nil, fmt.Errorf("failed to load keyshare: %w", err)
	}
	defer session.DklsKeyshareFree(keyshareHandle)

	// Create session from setup
	handle, err := session.DklsSignSessionFromSetup(setupData, []byte(partyID), keyshareHandle)
	if err != nil {
		return nil, fmt.Errorf("failed to create sign session: %w", err)
	}

	return &signSession{
		sessionID:    sessionID,
		partyID:      partyID,
		handle:       handle,
		payloadCh:    make(chan []byte, 256),
		participants: participants,
	}, nil
}

// Step processes the next protocol step and returns messages to send.
func (s *signSession) Step() ([]Message, bool, error) {
	// Process any queued input messages first
	select {
	case payload := <-s.payloadCh:
		finished, err := session.DklsSignSessionInputMessage(s.handle, payload)
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
		msgData, err := session.DklsSignSessionOutputMessage(s.handle)
		if err != nil {
			return nil, false, fmt.Errorf("failed to get output message: %w", err)
		}
		if len(msgData) == 0 {
			break
		}

		for idx := 0; idx < len(s.participants); idx++ {
			receiverBytes, err := session.DklsSignSessionMessageReceiver(s.handle, msgData, idx)
			if err != nil {
				return nil, false, fmt.Errorf("failed to get message receiver: %w", err)
			}
			receiver := string(receiverBytes)
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
func (s *signSession) enqueuePayload(data []byte) error {
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
func (s *signSession) InputMessage(data []byte) error {
	return s.enqueuePayload(data)
}

// GetParticipants returns the list of participant party IDs.
func (s *signSession) GetParticipants() []string {
	// Return a copy to avoid mutation
	participants := make([]string, len(s.participants))
	copy(participants, s.participants)
	return participants
}

// GetResult returns the result when finished.
func (s *signSession) GetResult() (*Result, error) {
	sig, err := session.DklsSignSessionFinish(s.handle)
	if err != nil {
		return nil, fmt.Errorf("failed to finish sign session: %w", err)
	}

	// Return participants list (copy to avoid mutation)
	participants := make([]string, len(s.participants))
	copy(participants, s.participants)

	return &Result{
		Keyshare:     nil,
		Signature:    sig,
		Participants: participants,
	}, nil
}

// Close cleans up the session.
func (s *signSession) Close() {
	if s.handle != 0 {
		session.DklsSignSessionFree(s.handle)
		s.handle = 0
	}
}
