package sessionmanager

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sync"
	"time"

	"github.com/pkg/errors"
	"github.com/rs/zerolog"

	"github.com/pushchain/push-chain-node/universalClient/store"
	"github.com/pushchain/push-chain-node/universalClient/tss/coordinator"
	"github.com/pushchain/push-chain-node/universalClient/tss/dkls"
	"github.com/pushchain/push-chain-node/universalClient/tss/eventstore"
	"github.com/pushchain/push-chain-node/universalClient/tss/keyshare"
)

// SendFunc is a function type for sending messages to participants.
type SendFunc func(ctx context.Context, peerID string, data []byte) error

// SessionType represents the type of TSS protocol session.
type SessionType string

const (
	SessionTypeKeygen     SessionType = "keygen"
	SessionTypeKeyrefresh SessionType = "keyrefresh"
	SessionTypeSign       SessionType = "sign"
)

// sessionState holds all state for a single session.
type sessionState struct {
	session      dkls.Session
	sessionType  SessionType // type of session (keygen, keyrefresh, sign)
	coordinator  string      // coordinatorPeerID
	expiryTime   time.Time   // when session expires
	participants []string    // list of participants (from setup message)
	stepMu       sync.Mutex  // mutex to serialize Step() calls (DKLS may not be thread-safe)
}

// SessionManager manages TSS protocol sessions and handles incoming messages.
type SessionManager struct {
	eventStore        *eventstore.Store
	coordinator       *coordinator.Coordinator
	keyshareManager   *keyshare.Manager
	send              SendFunc
	partyID           string // Our validator address
	logger            zerolog.Logger
	sessionExpiryTime time.Duration // How long a session can be inactive before expiring

	// Session storage
	mu       sync.RWMutex
	sessions map[string]*sessionState // eventID -> sessionState
}

// NewSessionManager creates a new session manager.
func NewSessionManager(
	eventStore *eventstore.Store,
	coord *coordinator.Coordinator,
	keyshareManager *keyshare.Manager,
	send SendFunc,
	partyID string,
	sessionExpiryTime time.Duration,
	logger zerolog.Logger,
) *SessionManager {
	return &SessionManager{
		eventStore:        eventStore,
		coordinator:       coord,
		keyshareManager:   keyshareManager,
		send:              send,
		partyID:           partyID,
		sessionExpiryTime: sessionExpiryTime,
		logger:            logger,
		sessions:          make(map[string]*sessionState),
	}
}

// HandleIncomingMessage handles an incoming message.
// peerID: The peer ID of the sender
// data: The raw message bytes (should be JSON-encoded coordinator.Message)
func (sm *SessionManager) HandleIncomingMessage(ctx context.Context, peerID string, data []byte) error {
	// Unmarshal message
	var msg coordinator.Message
	if err := json.Unmarshal(data, &msg); err != nil {
		return errors.Wrap(err, "failed to unmarshal message")
	}

	sm.logger.Debug().
		Str("peer_id", peerID).
		Str("type", msg.Type).
		Str("event_id", msg.EventID).
		Int("participants_count", len(msg.Participants)).
		Msg("handling incoming message")

	// Route based on message type
	switch msg.Type {
	case "setup":
		return sm.handleSetupMessage(ctx, peerID, &msg)
	case "begin":
		return sm.handleBeginMessage(ctx, peerID, &msg)
	case "step":
		return sm.handleStepMessage(ctx, peerID, &msg)
	default:
		return errors.Errorf("unknown message type: %s", msg.Type)
	}
}

// handleSetupMessage validates and processes a setup message.
func (sm *SessionManager) handleSetupMessage(ctx context.Context, senderPeerID string, msg *coordinator.Message) error {
	// 1. Validate event exists in DB
	event, err := sm.eventStore.GetEvent(msg.EventID)
	if err != nil {
		return errors.Wrapf(err, "event %s not found in database", msg.EventID)
	}

	// 2. Validate sender is coordinator
	isCoord, err := sm.coordinator.IsPeerCoordinator(ctx, senderPeerID)
	if err != nil {
		return errors.Wrap(err, "failed to check if sender is coordinator")
	}
	if !isCoord {
		return errors.Errorf("sender %s is not the coordinator", senderPeerID)
	}

	// 3. Validate participants list matches event protocol requirements
	if err := sm.validateParticipants(msg.Participants, event); err != nil {
		return errors.Wrap(err, "participants validation failed")
	}

	// 4. Check if session already exists
	sm.mu.Lock()
	if _, exists := sm.sessions[msg.EventID]; exists {
		sm.mu.Unlock()
		sm.logger.Warn().Str("event_id", msg.EventID).Msg("session already exists, ignoring setup")
		return nil
	}
	sm.mu.Unlock()

	// 5. Create session based on protocol type
	session, err := sm.createSession(ctx, event, msg)
	if err != nil {
		return errors.Wrapf(err, "failed to create session for event %s", msg.EventID)
	}

	// 6. Determine session type from event protocol type
	var sessionType SessionType
	switch event.ProtocolType {
	case "keygen":
		sessionType = SessionTypeKeygen
	case "keyrefresh":
		sessionType = SessionTypeKeyrefresh
	case "sign":
		sessionType = SessionTypeSign
	default:
		return errors.Errorf("unknown protocol type: %s", event.ProtocolType)
	}

	// 7. Store session state
	sm.mu.Lock()
	sm.sessions[msg.EventID] = &sessionState{
		session:      session,
		sessionType:  sessionType,
		coordinator:  senderPeerID,
		expiryTime:   time.Now().Add(sm.sessionExpiryTime),
		participants: msg.Participants,
	}
	sm.mu.Unlock()

	// 8. Update event status to IN_PROGRESS
	if err := sm.eventStore.UpdateStatus(msg.EventID, eventstore.StatusInProgress, ""); err != nil {
		sm.logger.Warn().Err(err).Str("event_id", msg.EventID).Msg("failed to update event status")
	}

	sm.logger.Info().
		Str("event_id", msg.EventID).
		Str("protocol", event.ProtocolType).
		Msg("created session from setup message")

	// 9. Send ACK to coordinator
	if err := sm.sendACK(ctx, senderPeerID, msg.EventID); err != nil {
		sm.logger.Warn().
			Err(err).
			Str("event_id", msg.EventID).
			Msg("failed to send ACK to coordinator")
		// Continue anyway - session is created
	}

	// Wait for BEGIN message from coordinator to start the protocol
	return nil
}

// handleStepMessage validates and processes a step message.
func (sm *SessionManager) handleStepMessage(ctx context.Context, senderPeerID string, msg *coordinator.Message) error {
	// 1. Get session state
	sm.mu.RLock()
	state, exists := sm.sessions[msg.EventID]
	sm.mu.RUnlock()

	if !exists {
		return errors.Errorf("session for event %s does not exist", msg.EventID)
	}

	session := state.session

	// 2. Validate sender is from session participants
	// Get participants from state
	sessionParticipants := state.participants

	// Get sender's validator address from peerID
	senderPartyID, err := sm.coordinator.GetPartyIDFromPeerID(ctx, senderPeerID)
	if err != nil {
		return errors.Wrapf(err, "failed to get partyID for sender peerID %s", senderPeerID)
	}

	// Check if sender is in participants
	isParticipant := false
	for _, p := range sessionParticipants {
		if p == senderPartyID {
			isParticipant = true
			break
		}
	}
	if !isParticipant {
		return errors.Errorf("sender %s (partyID: %s) is not in session participants for event %s", senderPeerID, senderPartyID, msg.EventID)
	}

	// 3. Route message to session
	if err := session.InputMessage(msg.Payload); err != nil {
		return errors.Wrapf(err, "failed to input message to session %s", msg.EventID)
	}

	// 4. Process step
	return sm.processSessionStep(ctx, msg.EventID)
}

// processSessionStep processes a step for the given session and sends output messages.
func (sm *SessionManager) processSessionStep(ctx context.Context, eventID string) error {
	sm.mu.RLock()
	state, exists := sm.sessions[eventID]
	sm.mu.RUnlock()

	if !exists {
		return errors.Errorf("session for event %s does not exist", eventID)
	}

	session := state.session

	// Step the session (serialize to prevent concurrent access - DKLS may not be thread-safe)
	state.stepMu.Lock()
	messages, finished, err := session.Step()
	state.stepMu.Unlock()

	if err != nil {
		return errors.Wrapf(err, "failed to step session %s", eventID)
	}

	// Send output messages
	for _, dklsMsg := range messages {
		// Find peerID for receiver partyID
		peerID, err := sm.coordinator.GetPeerIDFromPartyID(ctx, dklsMsg.Receiver)
		if err != nil {
			sm.logger.Warn().
				Err(err).
				Str("receiver_party_id", dklsMsg.Receiver).
				Msg("failed to get peerID for receiver")
			continue
		}

		// Create coordinator message
		coordMsg := coordinator.Message{
			Type:         "step",
			EventID:      eventID,
			Payload:      dklsMsg.Data,
			Participants: nil, // Participants not needed for step messages
		}
		msgBytes, err := json.Marshal(coordMsg)
		if err != nil {
			sm.logger.Warn().Err(err).Msg("failed to marshal step message")
			continue
		}

		// Send message
		if err := sm.send(ctx, peerID, msgBytes); err != nil {
			sm.logger.Warn().
				Err(err).
				Str("receiver", dklsMsg.Receiver).
				Str("peer_id", peerID).
				Msg("failed to send step message")
			continue
		}

		sm.logger.Debug().
			Str("event_id", eventID).
			Str("receiver", dklsMsg.Receiver).
			Msg("sent step message")
	}

	// If finished, handle result
	if finished {
		return sm.handleSessionFinished(ctx, eventID, state)
	}

	return nil
}

// cleanSession removes a session and all associated data.
// It closes the session and logs the cleanup.
func (sm *SessionManager) cleanSession(eventID string, state *sessionState) {
	sm.mu.Lock()
	delete(sm.sessions, eventID)
	sm.mu.Unlock()
	state.session.Close()
	sm.logger.Info().Str("event_id", eventID).Msg("session cleaned up")
}

// handleBeginMessage processes a begin message from the coordinator.
// This message signals that all participants have ACKed and the protocol should start.
func (sm *SessionManager) handleBeginMessage(ctx context.Context, senderPeerID string, msg *coordinator.Message) error {
	// 1. Get session state
	sm.mu.RLock()
	state, exists := sm.sessions[msg.EventID]
	sm.mu.RUnlock()

	if !exists {
		return errors.Errorf("session for event %s does not exist", msg.EventID)
	}

	// 2. Validate sender is the coordinator for this session
	if senderPeerID != state.coordinator {
		return errors.Errorf("begin message must come from coordinator %s, but received from %s", state.coordinator, senderPeerID)
	}

	sm.logger.Info().
		Str("event_id", msg.EventID).
		Str("coordinator", senderPeerID).
		Msg("received begin message, starting session processing")

	// 3. Start processing the session by triggering the first step
	return sm.processSessionStep(ctx, msg.EventID)
}

// sendACK sends an ACK message to the coordinator after successfully creating a session.
func (sm *SessionManager) sendACK(ctx context.Context, coordinatorPeerID string, eventID string) error {
	ackMsg := coordinator.Message{
		Type:         "ack",
		EventID:      eventID,
		Payload:      nil, // ACK doesn't need payload
		Participants: nil, // ACK doesn't need participants
	}
	msgBytes, err := json.Marshal(ackMsg)
	if err != nil {
		return errors.Wrap(err, "failed to marshal ACK message")
	}

	if err := sm.send(ctx, coordinatorPeerID, msgBytes); err != nil {
		return errors.Wrap(err, "failed to send ACK message")
	}

	sm.logger.Debug().
		Str("event_id", eventID).
		Str("coordinator", coordinatorPeerID).
		Msg("sent ACK to coordinator")

	return nil
}

// handleSessionFinished handles a completed session.
func (sm *SessionManager) handleSessionFinished(ctx context.Context, eventID string, state *sessionState) error {
	// Ensure session is cleaned up even on error
	defer sm.cleanSession(eventID, state)

	session := state.session

	// Get result
	result, err := session.GetResult()
	if err != nil {
		return errors.Wrapf(err, "failed to get result for session %s", eventID)
	}

	// Handle based on session type
	switch state.sessionType {
	case SessionTypeKeygen:
		// Save keyshare using keyID from result
		if err := sm.keyshareManager.Store(result.Keyshare, result.KeyID); err != nil {
			return errors.Wrapf(err, "failed to store keyshare for event %s", eventID)
		}
		// Calculate SHA256 hash of keyshare for verification
		keyshareHash := sha256.Sum256(result.Keyshare)
		sm.logger.Info().
			Str("event_id", eventID).
			Str("key_id", result.KeyID).
			Str("public_key", hex.EncodeToString(result.PublicKey)).
			Str("keyshare_hash", hex.EncodeToString(keyshareHash[:])).
			Msg("saved keyshare from keygen")

	case SessionTypeKeyrefresh:
		// Save new keyshare using keyID from result
		if err := sm.keyshareManager.Store(result.Keyshare, result.KeyID); err != nil {
			return errors.Wrapf(err, "failed to store keyshare for event %s", eventID)
		}
		// Calculate SHA256 hash of keyshare for verification
		keyshareHash := sha256.Sum256(result.Keyshare)
		sm.logger.Info().
			Str("event_id", eventID).
			Str("key_id", result.KeyID).
			Str("public_key", hex.EncodeToString(result.PublicKey)).
			Str("keyshare_hash", hex.EncodeToString(keyshareHash[:])).
			Msg("saved new keyshare from keyrefresh")

	case SessionTypeSign:
		// TODO: Save signature to database for outbound Tx Processing
		sm.logger.Info().
			Str("event_id", eventID).
			Str("signature", hex.EncodeToString(result.Signature)).
			Str("key_id", result.KeyID).
			Str("public_key", hex.EncodeToString(result.PublicKey)).
			Msg("signature generated and verified from sign session")

	default:
		return errors.Errorf("unknown session type: %s", state.sessionType)
	}

	// Update event status to SUCCESS (common for all session types)
	if err := sm.eventStore.UpdateStatus(eventID, eventstore.StatusSuccess, ""); err != nil {
		return errors.Wrapf(err, "failed to update event status")
	}

	sm.logger.Info().Str("event_id", eventID).Msg("session finished successfully")

	return nil
}

// createSession creates a new DKLS session based on event type.
func (sm *SessionManager) createSession(ctx context.Context, event *store.TSSEvent, msg *coordinator.Message) (dkls.Session, error) {
	threshold := coordinator.CalculateThreshold(len(msg.Participants))

	switch event.ProtocolType {
	case "keygen":
		return dkls.NewKeygenSession(
			msg.Payload, // setupData
			msg.EventID, // sessionID
			sm.partyID,
			msg.Participants,
			threshold,
		)

	case "keyrefresh":
		// Get current keyID
		keyID, err := sm.coordinator.GetCurrentTSSKeyId(ctx)
		if err != nil {
			return nil, errors.Wrap(err, "failed to get current TSS keyId")
		}

		// Load old keyshare
		oldKeyshare, err := sm.keyshareManager.Get(keyID)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to load keyshare for keyId %s", keyID)
		}

		return dkls.NewKeyrefreshSession(
			msg.Payload, // setupData
			msg.EventID, // sessionID
			sm.partyID,
			msg.Participants,
			threshold,
			oldKeyshare,
		)

	case "sign":
		// Get current keyID
		keyID, err := sm.coordinator.GetCurrentTSSKeyId(ctx)
		if err != nil {
			return nil, errors.Wrap(err, "failed to get current TSS keyId")
		}

		// Load keyshare
		keyshareBytes, err := sm.keyshareManager.Get(keyID)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to load keyshare for keyId %s", keyID)
		}

		// Extract message hash from event data
		messageHash, err := extractMessageHash(event.EventData)
		if err != nil {
			return nil, errors.Wrap(err, "failed to extract message hash")
		}

		return dkls.NewSignSession(
			msg.Payload, // setupData
			msg.EventID, // sessionID
			sm.partyID,
			msg.Participants,
			keyshareBytes,
			messageHash,
			nil, // chainPath
		)

	default:
		return nil, errors.Errorf("unknown protocol type: %s", event.ProtocolType)
	}
}

// validateParticipants validates that participants match protocol requirements.
// For keygen/keyrefresh: participants must match exactly with eligible participants (same elements).
// For sign: participants must be a valid >2/3 subset of eligible participants.
func (sm *SessionManager) validateParticipants(participants []string, event *store.TSSEvent) error {
	// Get eligible validators for this protocol
	eligible := sm.coordinator.GetEligibleUV(string(event.ProtocolType))
	if len(eligible) == 0 {
		return errors.New("no eligible validators for protocol")
	}

	// Build set and list of eligible partyIDs
	eligibleSet := make(map[string]bool)
	eligibleList := make([]string, 0, len(eligible))
	for _, v := range eligible {
		eligibleSet[v.ValidatorAddress] = true
		eligibleList = append(eligibleList, v.ValidatorAddress)
	}

	// Validate all participants are eligible
	participantSet := make(map[string]bool)
	for _, partyID := range participants {
		if !eligibleSet[partyID] {
			return errors.Errorf("participant %s is not eligible for protocol %s", partyID, event.ProtocolType)
		}
		participantSet[partyID] = true
	}

	// Protocol-specific validation
	switch event.ProtocolType {
	case "keygen", "keyrefresh":
		// For keygen and keyrefresh: participants must match exactly with eligible participants
		if len(participants) != len(eligibleList) {
			return errors.Errorf("participants count %d does not match eligible count %d for %s", len(participants), len(eligibleList), event.ProtocolType)
		}
		// Check all eligible are in participants
		for _, eligibleID := range eligibleList {
			if !participantSet[eligibleID] {
				return errors.Errorf("eligible participant %s is missing from participants list for %s", eligibleID, event.ProtocolType)
			}
		}

	case "sign":
		// For sign: participants must be exactly equal to threshold (no more, no less)
		threshold := coordinator.CalculateThreshold(len(eligibleList))
		if len(participants) != threshold {
			return errors.Errorf("participants count %d must equal threshold %d (required >2/3 of %d eligible) for sign", len(participants), threshold, len(eligibleList))
		}
		// All participants must be from eligible set (already validated above)

	default:
		return errors.Errorf("unknown protocol type: %s", event.ProtocolType)
	}

	return nil
}

// extractMessageHash extracts the message hash from event data.
func extractMessageHash(eventData []byte) ([]byte, error) {
	// Try to parse as JSON first
	var eventDataJSON map[string]interface{}
	if err := json.Unmarshal(eventData, &eventDataJSON); err == nil {
		// Successfully parsed as JSON, try to get "message" field
		if msg, ok := eventDataJSON["message"].(string); ok {
			// Hash the message
			hash := sha256.Sum256([]byte(msg))
			return hash[:], nil
		}
		return nil, errors.New("event data JSON does not contain 'message' string field")
	}

	// Not JSON, treat eventData as the message string directly
	if len(eventData) == 0 {
		return nil, errors.New("message is empty")
	}

	// Hash the message
	hash := sha256.Sum256(eventData)
	return hash[:], nil
}

// StartExpiryChecker starts a background goroutine that periodically checks for expired sessions.
func (sm *SessionManager) StartExpiryChecker(ctx context.Context, checkInterval time.Duration, blockDelay uint64) {
	if checkInterval == 0 {
		checkInterval = 30 * time.Second
	}
	if blockDelay == 0 {
		blockDelay = 60 // Default: retry after 60 blocks ( Approx 1 Minute for PC)
	}

	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sm.checkExpiredSessions(ctx, blockDelay)
		}
	}
}

// checkExpiredSessions checks for expired sessions and marks their events as pending for retry.
func (sm *SessionManager) checkExpiredSessions(ctx context.Context, blockDelay uint64) {
	now := time.Now()
	var expiredSessions []string

	// Find expired sessions
	sm.mu.RLock()
	for eventID, state := range sm.sessions {
		if now.After(state.expiryTime) {
			expiredSessions = append(expiredSessions, eventID)
		}
	}
	sm.mu.RUnlock()

	// Process expired sessions
	for _, eventID := range expiredSessions {
		sm.mu.Lock()
		state, hasSession := sm.sessions[eventID]
		sm.mu.Unlock()

		if hasSession {
			// Get current block number from coordinator
			currentBlock, err := sm.coordinator.GetLatestBlockNum(ctx)
			if err != nil {
				sm.logger.Warn().
					Err(err).
					Str("event_id", eventID).
					Msg("failed to get current block number for expired session")
				continue
			}

			// Clean up session
			sm.cleanSession(eventID, state)

			// Update event: mark as pending and set new block number (current + delay)
			newBlockNumber := currentBlock + blockDelay
			if err := sm.eventStore.UpdateStatusAndBlockNumber(eventID, eventstore.StatusPending, newBlockNumber); err != nil {
				sm.logger.Warn().
					Err(err).
					Str("event_id", eventID).
					Msg("failed to update expired session event")
			} else {
				sm.logger.Info().
					Str("event_id", eventID).
					Uint64("new_block_number", newBlockNumber).
					Msg("expired session removed, event marked as pending for retry")
			}
		}
	}
}
