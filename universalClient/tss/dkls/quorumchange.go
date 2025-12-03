package dkls

import (
	"fmt"

	session "go-wrapper/go-dkls/sessions"
)

// quorumchangeSession implements Session.
type quorumchangeSession struct {
	sessionID    string
	partyID      string
	handle       session.Handle
	payloadCh    chan []byte
	participants []string
}

// NewQuorumChangeSession creates a new quorumchange session.
// setupData: The setup message (required - must be provided by caller)
// sessionID: Session identifier (typically eventID)
// partyID: This node's party ID
// participants: List of participant party IDs (sorted)
// threshold: The threshold for the quorumchange
// oldKeyshare: The existing keyshare to change quorum for (nil if this is a new party)
func NewQuorumChangeSession(
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

	var oldHandle session.Handle
	var err error

	// If oldKeyshare is provided, load it (existing party)
	// If nil or empty, we're a new party joining the quorum
	if len(oldKeyshare) > 0 {
		oldHandle, err = session.DklsKeyshareFromBytes(oldKeyshare)
		if err != nil {
			return nil, fmt.Errorf("failed to load old keyshare: %w", err)
		}
		// Note: We don't free oldHandle here because DklsQcSessionFromSetup
		// may need it during session creation. The session will manage its own copy.
	} else {
		// New party - no old keyshare
		oldHandle = 0
	}

	// Create session from setup
	// Note: Quorumchange session creation - function expects partyID as string
	// oldHandle can be 0 (nil) for new parties
	handle, err := session.DklsQcSessionFromSetup(setupData, partyID, oldHandle)
	if err != nil {
		// Free oldHandle if session creation failed
		if oldHandle != 0 {
			session.DklsKeyshareFree(oldHandle)
		}
		return nil, fmt.Errorf("failed to create quorumchange session: %w", err)
	}

	// Free oldHandle after session is created (session has its own copy)
	if oldHandle != 0 {
		session.DklsKeyshareFree(oldHandle)
	}

	return &quorumchangeSession{
		sessionID:    sessionID,
		partyID:      partyID,
		handle:       handle,
		payloadCh:    make(chan []byte, 256),
		participants: participants,
	}, nil
}

// Step processes the next protocol step and returns messages to send.
func (s *quorumchangeSession) Step() ([]Message, bool, error) {
	// Process any queued input messages first
	select {
	case payload := <-s.payloadCh:
		finished, err := session.DklsQcSessionInputMessage(s.handle, payload)
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
		msgData, err := session.DklsQcSessionOutputMessage(s.handle)
		if err != nil {
			return nil, false, fmt.Errorf("failed to get output message: %w", err)
		}
		if len(msgData) == 0 {
			break
		}

		for idx := 0; idx < len(s.participants); idx++ {
			receiver, err := session.DklsQcSessionMessageReceiver(s.handle, msgData, idx)
			if err != nil {
				return nil, false, fmt.Errorf("failed to get message receiver: %w", err)
			}
			if receiver == "" {
				break
			}

			if receiver == s.partyID {
				if err := s.InputMessage(msgData); err != nil {
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

// InputMessage processes an incoming protocol message.
func (s *quorumchangeSession) InputMessage(data []byte) error {
	buf := make([]byte, len(data))
	copy(buf, data)
	select {
	case s.payloadCh <- buf:
		return nil
	default:
		return fmt.Errorf("payload buffer full for session %s", s.sessionID)
	}
}

// GetResult returns the result when finished.
func (s *quorumchangeSession) GetResult() (*Result, error) {
	// Finish the session - quorumchange produces a new keyshare
	keyHandle, err := session.DklsQcSessionFinish(s.handle)
	if err != nil {
		return nil, fmt.Errorf("failed to finish quorumchange session: %w", err)
	}
	defer session.DklsKeyshareFree(keyHandle)

	keyshare, err := session.DklsKeyshareToBytes(keyHandle)
	if err != nil {
		return nil, fmt.Errorf("failed to extract keyshare: %w", err)
	}

	// Extract publicKey from keyshare handle
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
		PublicKey:    publicKey,
		Participants: participants,
	}, nil
}

// Close cleans up the session.
func (s *quorumchangeSession) Close() {
	if s.handle != 0 {
		// Just reset the handle - the underlying resources are managed by the library
		s.handle = 0
	}
}
