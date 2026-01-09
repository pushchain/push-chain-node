package sessionmanager

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"strconv"
	"sync"
	"time"

	"github.com/pkg/errors"
	"github.com/rs/zerolog"

	"github.com/pushchain/push-chain-node/universalClient/chains/common"
	"github.com/pushchain/push-chain-node/universalClient/pushcore"
	"github.com/pushchain/push-chain-node/universalClient/store"
	"github.com/pushchain/push-chain-node/universalClient/tss/coordinator"
	"github.com/pushchain/push-chain-node/universalClient/tss/dkls"
	"github.com/pushchain/push-chain-node/universalClient/tss/eventstore"
	"github.com/pushchain/push-chain-node/universalClient/tss/keyshare"
	"github.com/pushchain/push-chain-node/universalClient/tss/vote"
	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
)

// SendFunc is a function type for sending messages to participants.
type SendFunc func(ctx context.Context, peerID string, data []byte) error

// sessionState holds all state for a single session.
type sessionState struct {
	session      dkls.Session
	protocolType string     // type of protocol (keygen, keyrefresh, quorumchange, sign)
	coordinator  string     // coordinatorPeerID
	expiryTime   time.Time  // when session expires
	participants []string   // list of participants (from setup message)
	stepMu       sync.Mutex // mutex to serialize Step() calls (DKLS may not be thread-safe)
}

// SessionManager manages TSS protocol sessions and handles incoming messages.
type SessionManager struct {
	eventStore        *eventstore.Store
	coordinator       *coordinator.Coordinator
	keyshareManager   *keyshare.Manager
	pushCore          *pushcore.Client                // For validating gas prices
	txBuilderFactory  common.OutboundTxBuilderFactory // For building tx to verify hash
	send              SendFunc
	partyID           string // Our validator address (pushvaloper format)
	logger            zerolog.Logger
	sessionExpiryTime time.Duration // How long a session can be inactive before expiring
	voteHandler       *vote.Handler // Optional - nil if voting disabled

	// Session storage
	mu       sync.RWMutex
	sessions map[string]*sessionState // eventID -> sessionState
}

// NewSessionManager creates a new session manager.
func NewSessionManager(
	eventStore *eventstore.Store,
	coord *coordinator.Coordinator,
	keyshareManager *keyshare.Manager,
	pushCore *pushcore.Client,
	txBuilderFactory common.OutboundTxBuilderFactory,
	send SendFunc,
	partyID string,
	sessionExpiryTime time.Duration,
	logger zerolog.Logger,
	voteHandler *vote.Handler, // Optional - nil if voting disabled
) *SessionManager {
	return &SessionManager{
		eventStore:        eventStore,
		coordinator:       coord,
		keyshareManager:   keyshareManager,
		pushCore:          pushCore,
		txBuilderFactory:  txBuilderFactory,
		send:              send,
		partyID:           partyID,
		sessionExpiryTime: sessionExpiryTime,
		logger:            logger,
		voteHandler:       voteHandler,
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

	// 4.5. For SIGN events, verify the signing hash independently
	if event.Type == string(coordinator.ProtocolSign) {
		if err := sm.verifySignMetadata(ctx, event, msg.SignMetadata); err != nil {
			return errors.Wrap(err, "sign metadata verification failed")
		}
	}

	// 5. Create session based on protocol type
	session, err := sm.createSession(ctx, event, msg)
	if err != nil {
		return errors.Wrapf(err, "failed to create session for event %s", msg.EventID)
	}

	// 6. Store session state
	sm.mu.Lock()
	sm.sessions[msg.EventID] = &sessionState{
		session:      session,
		protocolType: event.Type,
		coordinator:  senderPeerID,
		expiryTime:   time.Now().Add(sm.sessionExpiryTime),
		participants: msg.Participants,
	}
	sm.mu.Unlock()

	// 7. Update event status to IN_PROGRESS
	if err := sm.eventStore.UpdateStatus(msg.EventID, eventstore.StatusInProgress, ""); err != nil {
		sm.logger.Warn().Err(err).Str("event_id", msg.EventID).Msg("failed to update event status")
	}

	sm.logger.Info().
		Str("event_id", msg.EventID).
		Str("protocol", event.Type).
		Msg("created session from setup message")

	// 8. Send ACK to coordinator
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

	// Use SHA256 hash of eventID as the storage identifier
	eventIDHash := sha256.Sum256([]byte(eventID))
	storageID := hex.EncodeToString(eventIDHash[:])

	// Handle based on protocol type
	switch state.protocolType {
	case string(coordinator.ProtocolKeygen):
		// Save keyshare using SHA256 hash of eventID
		if err := sm.keyshareManager.Store(result.Keyshare, storageID); err != nil {
			return errors.Wrapf(err, "failed to store keyshare for event %s", eventID)
		}
		// Calculate SHA256 hash of keyshare for verification
		keyshareHash := sha256.Sum256(result.Keyshare)
		sm.logger.Info().
			Str("event_id", eventID).
			Str("storage_id", storageID).
			Str("public_key", hex.EncodeToString(result.PublicKey)).
			Str("keyshare_hash", hex.EncodeToString(keyshareHash[:])).
			Msg("saved keyshare from keygen")

	case string(coordinator.ProtocolKeyrefresh):
		// Save new keyshare using SHA256 hash of eventID
		if err := sm.keyshareManager.Store(result.Keyshare, storageID); err != nil {
			return errors.Wrapf(err, "failed to store keyshare for event %s", eventID)
		}
		// Calculate SHA256 hash of keyshare for verification
		keyshareHash := sha256.Sum256(result.Keyshare)
		sm.logger.Info().
			Str("event_id", eventID).
			Str("storage_id", storageID).
			Str("public_key", hex.EncodeToString(result.PublicKey)).
			Str("keyshare_hash", hex.EncodeToString(keyshareHash[:])).
			Msg("saved new keyshare from keyrefresh")

	case string(coordinator.ProtocolQuorumChange):
		// Quorumchange produces a new keyshare
		// Save new keyshare using SHA256 hash of eventID
		if err := sm.keyshareManager.Store(result.Keyshare, storageID); err != nil {
			return errors.Wrapf(err, "failed to store keyshare for event %s", eventID)
		}
		// Calculate SHA256 hash of keyshare for verification
		keyshareHash := sha256.Sum256(result.Keyshare)
		sm.logger.Info().
			Str("event_id", eventID).
			Str("storage_id", storageID).
			Str("public_key", hex.EncodeToString(result.PublicKey)).
			Str("keyshare_hash", hex.EncodeToString(keyshareHash[:])).
			Int("participant_count", len(result.Participants)).
			Msg("saved new keyshare from quorumchange with updated participants")

	case string(coordinator.ProtocolSign):
		// TODO: Save signature to database for outbound Tx Processing
		sm.logger.Info().
			Str("event_id", eventID).
			Str("signature", hex.EncodeToString(result.Signature)).
			Str("public_key", hex.EncodeToString(result.PublicKey)).
			Msg("signature generated and verified from sign session")

	default:
		return errors.Errorf("unknown protocol type: %s", state.protocolType)
	}

	// Vote on TSS key process (keygen/keyrefresh/quorumchange only)
	if sm.voteHandler != nil && (state.protocolType == string(coordinator.ProtocolKeygen) || state.protocolType == string(coordinator.ProtocolKeyrefresh) || state.protocolType == string(coordinator.ProtocolQuorumChange)) {
		pubKeyHex := hex.EncodeToString(result.PublicKey)

		paEventIDInt, err := strconv.ParseUint(eventID, 10, 64)
		if err != nil {
			return errors.Wrapf(err, "failed to parse process id from %s", eventID)
		}
		voteTxHash, err := sm.voteHandler.VoteTssKeyProcess(ctx, pubKeyHex, storageID, paEventIDInt)
		if err != nil {
			sm.logger.Warn().Err(err).Str("event_id", eventID).Msg("TSS vote failed - marking PENDING")

			if err := sm.eventStore.UpdateStatus(eventID, eventstore.StatusPending, err.Error()); err != nil {
				return errors.Wrapf(err, "failed to update event status to PENDING")
			}
			return nil // Event will be retried
		}

		sm.logger.Info().Str("vote_tx_hash", voteTxHash).Str("event_id", eventID).Msg("TSS vote succeeded")
	}

	// Update event status to SUCCESS (only reached if vote succeeded or not required)
	if err := sm.eventStore.UpdateStatus(eventID, eventstore.StatusSuccess, ""); err != nil {
		return errors.Wrapf(err, "failed to update event status")
	}

	sm.logger.Info().Str("event_id", eventID).Msg("session finished successfully")

	return nil
}

// createSession creates a new DKLS session based on event type.
func (sm *SessionManager) createSession(ctx context.Context, event *store.PCEvent, msg *coordinator.Message) (dkls.Session, error) {
	threshold := coordinator.CalculateThreshold(len(msg.Participants))

	switch event.Type {
	case string(coordinator.ProtocolKeygen):
		return dkls.NewKeygenSession(
			msg.Payload, // setupData
			msg.EventID, // sessionID
			sm.partyID,
			msg.Participants,
			threshold,
		)

	case string(coordinator.ProtocolKeyrefresh):
		// Get current keyID
		keyID, err := sm.coordinator.GetCurrentTSSKeyId()
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

	case string(coordinator.ProtocolQuorumChange):
		// Get current keyID
		keyID, err := sm.coordinator.GetCurrentTSSKeyId()
		if err != nil {
			return nil, errors.Wrap(err, "failed to get current TSS keyId for quorumchange")
		}

		// Load old keyshare - if not found, we're a new party (oldKeyshare will be nil)
		oldKeyshare, err := sm.keyshareManager.Get(keyID)
		if err != nil {
			// Check if it's a "not found" error (new party) vs other error
			if err == keyshare.ErrKeyshareNotFound {
				// If keyshare not found, we're a new party joining the quorum
				// This is expected for new participants in quorumchange
				sm.logger.Info().
					Str("key_id", keyID).
					Str("party_id", sm.partyID).
					Msg("old keyshare not found for quorumchange - treating as new party")
				oldKeyshare = nil
			} else {
				// Other error (decryption failed, etc.) - return error
				return nil, errors.Wrapf(err, "failed to load keyshare for keyId %s", keyID)
			}
		}

		return dkls.NewQuorumChangeSession(
			msg.Payload, // setupData
			msg.EventID, // sessionID
			sm.partyID,
			msg.Participants,
			threshold,
			oldKeyshare,
		)

	case string(coordinator.ProtocolSign):
		// Get current keyID
		keyID, err := sm.coordinator.GetCurrentTSSKeyId()
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
		return nil, errors.Errorf("unknown protocol type: %s", event.Type)
	}
}

// validateParticipants validates that participants match protocol requirements.
// For keygen/keyrefresh: participants must match exactly with eligible participants (same elements).
// For sign: participants must be a valid >2/3 subset of eligible participants.
func (sm *SessionManager) validateParticipants(participants []string, event *store.PCEvent) error {
	// Get eligible validators for this protocol
	eligible := sm.coordinator.GetEligibleUV(string(event.Type))
	if len(eligible) == 0 {
		return errors.New("no eligible validators for protocol")
	}

	// Build set and list of eligible partyIDs
	eligibleSet := make(map[string]bool)
	eligibleList := make([]string, 0, len(eligible))
	for _, v := range eligible {
		if v.IdentifyInfo != nil {
			addr := v.IdentifyInfo.CoreValidatorAddress
			eligibleSet[addr] = true
			eligibleList = append(eligibleList, addr)
		}
	}

	// Validate all participants are eligible
	participantSet := make(map[string]bool)
	for _, partyID := range participants {
		if !eligibleSet[partyID] {
			return errors.Errorf("participant %s is not eligible for protocol %s", partyID, event.Type)
		}
		participantSet[partyID] = true
	}

	// Protocol-specific validation
	switch event.Type {
	case string(coordinator.ProtocolKeygen), string(coordinator.ProtocolKeyrefresh), string(coordinator.ProtocolQuorumChange):
		// For keygen, keyrefresh, and quorumchange: participants must match exactly with eligible participants
		if len(participants) != len(eligibleList) {
			return errors.Errorf("participants count %d does not match eligible count %d for %s", len(participants), len(eligibleList), event.Type)
		}
		// Check all eligible are in participants
		for _, eligibleID := range eligibleList {
			if !participantSet[eligibleID] {
				return errors.Errorf("eligible participant %s is missing from participants list for %s", eligibleID, event.Type)
			}
		}

	case string(coordinator.ProtocolSign):
		// For sign: participants must be exactly equal to threshold (no more, no less)
		threshold := coordinator.CalculateThreshold(len(eligibleList))
		if len(participants) != threshold {
			return errors.Errorf("participants count %d must equal threshold %d (required >2/3 of %d eligible) for sign", len(participants), threshold, len(eligibleList))
		}
		// All participants must be from eligible set (already validated above)

	default:
		return errors.Errorf("unknown protocol type: %s", event.Type)
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
			currentBlock, err := sm.coordinator.GetLatestBlockNum()
			if err != nil {
				sm.logger.Warn().
					Err(err).
					Str("event_id", eventID).
					Msg("failed to get current block number for expired session")
				continue
			}

			// Clean up session
			sm.cleanSession(eventID, state)

			// Update event: mark as pending and set new block height (current + delay)
			newBlockHeight := currentBlock + blockDelay
			if err := sm.eventStore.UpdateStatusAndBlockHeight(eventID, eventstore.StatusPending, newBlockHeight); err != nil {
				sm.logger.Warn().
					Err(err).
					Str("event_id", eventID).
					Msg("failed to update expired session event")
			} else {
				sm.logger.Info().
					Str("event_id", eventID).
					Uint64("new_block_height", newBlockHeight).
					Msg("expired session removed, event marked as pending for retry")
			}
		}
	}
}

// GasPriceTolerancePercent defines the acceptable deviation from oracle gas price (e.g., 10 = 10%)
const GasPriceTolerancePercent = 10

// verifySignMetadata validates the coordinator's signing request by:
// 1. Verifying the gas price is within acceptable range of on-chain oracle
// 2. Building the transaction independently using the same gas price
// 3. Comparing the resulting hash with coordinator's hash - must match exactly
func (sm *SessionManager) verifySignMetadata(ctx context.Context, event *store.PCEvent, meta *coordinator.SignMetadata) error {
	if meta == nil {
		return errors.New("sign metadata is required for SIGN events")
	}

	if meta.GasPrice == nil {
		return errors.New("gas price is missing in metadata")
	}

	if len(meta.SigningHash) == 0 {
		return errors.New("signing hash is missing in metadata")
	}

	// Parse the event data to get outbound transaction details
	var outboundData uexecutortypes.OutboundCreatedEvent
	if err := json.Unmarshal(event.EventData, &outboundData); err != nil {
		return errors.Wrap(err, "failed to parse outbound event data")
	}

	// 1. Validate gas price is reasonable (within tolerance of oracle price)
	if err := sm.validateGasPrice(ctx, outboundData.DestinationChain, meta.GasPrice); err != nil {
		return errors.Wrap(err, "gas price validation failed")
	}

	// 2. Build the transaction independently using the same gas price
	if sm.txBuilderFactory == nil {
		sm.logger.Warn().Msg("txBuilderFactory not configured, skipping hash verification")
		return nil
	}

	builder, err := sm.txBuilderFactory.CreateBuilder(outboundData.DestinationChain)
	if err != nil {
		return errors.Wrapf(err, "failed to create tx builder for chain %s", outboundData.DestinationChain)
	}

	// Build transaction with the coordinator's gas price
	txResult, err := builder.BuildTransaction(ctx, &outboundData, meta.GasPrice)
	if err != nil {
		return errors.Wrap(err, "failed to build transaction for verification")
	}

	// 3. Compare hashes - must match exactly
	if !bytes.Equal(txResult.SigningHash, meta.SigningHash) {
		sm.logger.Error().
			Str("our_hash", hex.EncodeToString(txResult.SigningHash)).
			Str("coordinator_hash", hex.EncodeToString(meta.SigningHash)).
			Str("event_id", event.EventID).
			Msg("signing hash mismatch - rejecting signing request")
		return errors.New("signing hash mismatch: our computed hash does not match coordinator's hash")
	}

	sm.logger.Debug().
		Str("event_id", event.EventID).
		Str("gas_price", meta.GasPrice.String()).
		Str("signing_hash", hex.EncodeToString(meta.SigningHash)).
		Msg("sign metadata verified - hash matches")

	return nil
}

// validateGasPrice checks that the provided gas price is within acceptable bounds of the oracle price.
func (sm *SessionManager) validateGasPrice(ctx context.Context, chainID string, gasPrice *big.Int) error {
	if sm.pushCore == nil {
		sm.logger.Warn().Msg("pushCore not configured, skipping gas price validation")
		return nil
	}

	if gasPrice == nil {
		return errors.New("gas price is nil")
	}

	// Get the current oracle gas price
	oraclePrice, err := sm.pushCore.GetGasPrice(ctx, chainID)
	if err != nil {
		return errors.Wrap(err, "failed to get oracle gas price")
	}

	// Check if gas price is within tolerance
	// Allow coordinator's price to be within ±GasPriceTolerancePercent of oracle price
	tolerance := new(big.Int).Div(oraclePrice, big.NewInt(100/GasPriceTolerancePercent))
	minPrice := new(big.Int).Sub(oraclePrice, tolerance)
	maxPrice := new(big.Int).Add(oraclePrice, tolerance)

	if gasPrice.Cmp(minPrice) < 0 {
		return errors.Errorf("gas price %s is too low (min: %s, oracle: %s)", gasPrice.String(), minPrice.String(), oraclePrice.String())
	}
	if gasPrice.Cmp(maxPrice) > 0 {
		return errors.Errorf("gas price %s is too high (max: %s, oracle: %s)", gasPrice.String(), maxPrice.String(), oraclePrice.String())
	}

	return nil
}
