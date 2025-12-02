package coordinator

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"sort"
	"sync"
	"time"

	"github.com/pkg/errors"
	"github.com/rs/zerolog"

	session "go-wrapper/go-dkls/sessions"

	"github.com/pushchain/push-chain-node/universalClient/store"
	"github.com/pushchain/push-chain-node/universalClient/tss/eventstore"
	"github.com/pushchain/push-chain-node/universalClient/tss/keyshare"
)

// Coordinator handles coordinator logic for TSS events.
type Coordinator struct {
	eventStore       *eventstore.Store
	dataProvider     DataProvider
	keyshareManager  *keyshare.Manager
	validatorAddress string
	coordinatorRange uint64
	pollInterval     time.Duration
	logger           zerolog.Logger
	send             SendFunc

	// Internal state
	mu            sync.RWMutex
	running       bool
	stopCh        chan struct{}
	allValidators []*UniversalValidator // Cached validators, updated at polling interval

	// ACK tracking for events we're coordinating (even if not participant)
	ackTracking map[string]*ackState // eventID -> ackState
	ackMu       sync.RWMutex
}

// ackState tracks ACK status for an event
type ackState struct {
	participants []string        // List of participant partyIDs
	ackedBy      map[string]bool // participant peerID -> has ACKed
	ackCount     int
}

// NewCoordinator creates a new coordinator.
func NewCoordinator(
	eventStore *eventstore.Store,
	dataProvider DataProvider,
	keyshareManager *keyshare.Manager,
	validatorAddress string,
	coordinatorRange uint64,
	pollInterval time.Duration,
	send SendFunc,
	logger zerolog.Logger,
) *Coordinator {
	if pollInterval == 0 {
		pollInterval = 10 * time.Second
	}
	return &Coordinator{
		eventStore:       eventStore,
		dataProvider:     dataProvider,
		keyshareManager:  keyshareManager,
		validatorAddress: validatorAddress,
		coordinatorRange: coordinatorRange,
		pollInterval:     pollInterval,
		logger:           logger,
		send:             send,
		stopCh:           make(chan struct{}),
		ackTracking:      make(map[string]*ackState),
	}
}

// GetPartyIDFromPeerID gets the partyID (validator address) for a given peerID.
// Uses cached allValidators for performance.
func (c *Coordinator) GetPartyIDFromPeerID(ctx context.Context, peerID string) (string, error) {
	// Use cached validators
	c.mu.RLock()
	allValidators := c.allValidators
	c.mu.RUnlock()

	if len(allValidators) == 0 {
		// If cache is empty, try to update it
		c.updateValidators(ctx)
		c.mu.RLock()
		allValidators = c.allValidators
		c.mu.RUnlock()
	}

	for _, v := range allValidators {
		if v.Network.PeerID == peerID {
			return v.ValidatorAddress, nil
		}
	}

	return "", errors.Errorf("peerID %s not found in validators", peerID)
}

// GetPeerIDFromPartyID gets the peerID for a given partyID (validator address).
// Uses cached allValidators for performance.
func (c *Coordinator) GetPeerIDFromPartyID(ctx context.Context, partyID string) (string, error) {
	// Use cached validators
	c.mu.RLock()
	allValidators := c.allValidators
	c.mu.RUnlock()

	if len(allValidators) == 0 {
		// If cache is empty, try to update it
		c.updateValidators(ctx)
		c.mu.RLock()
		allValidators = c.allValidators
		c.mu.RUnlock()
	}

	for _, v := range allValidators {
		if v.ValidatorAddress == partyID {
			return v.Network.PeerID, nil
		}
	}

	return "", errors.Errorf("partyID %s not found in validators", partyID)
}

// GetMultiAddrsFromPeerID gets the multiaddrs for a given peerID.
// Uses cached allValidators for performance.
func (c *Coordinator) GetMultiAddrsFromPeerID(ctx context.Context, peerID string) ([]string, error) {
	// Use cached validators
	c.mu.RLock()
	allValidators := c.allValidators
	c.mu.RUnlock()

	if len(allValidators) == 0 {
		// If cache is empty, try to update it
		c.updateValidators(ctx)
		c.mu.RLock()
		allValidators = c.allValidators
		c.mu.RUnlock()
	}

	for _, v := range allValidators {
		if v.Network.PeerID == peerID {
			return v.Network.Multiaddrs, nil
		}
	}

	return nil, errors.Errorf("peerID %s not found in validators", peerID)
}

// GetLatestBlockNum gets the latest block number from the data provider.
func (c *Coordinator) GetLatestBlockNum(ctx context.Context) (uint64, error) {
	return c.dataProvider.GetLatestBlockNum(ctx)
}

// IsPeerCoordinator checks if the given peerID is the coordinator for the current block.
// Uses cached allValidators for performance.
func (c *Coordinator) IsPeerCoordinator(ctx context.Context, peerID string) (bool, error) {
	currentBlock, err := c.dataProvider.GetLatestBlockNum(ctx)
	if err != nil {
		return false, errors.Wrap(err, "failed to get latest block number")
	}

	// Use cached validators
	c.mu.RLock()
	allValidators := c.allValidators
	c.mu.RUnlock()

	if len(allValidators) == 0 {
		return false, nil
	}

	// Find validator by peerID
	var validatorAddress string
	for _, v := range allValidators {
		if v.Network.PeerID == peerID {
			validatorAddress = v.ValidatorAddress
			break
		}
	}

	if validatorAddress == "" {
		return false, nil // Peer not found
	}

	coordinatorParticipants := getCoordinatorParticipants(allValidators)
	if len(coordinatorParticipants) == 0 {
		return false, nil
	}

	// Check if validator is coordinator for current block
	epoch := currentBlock / c.coordinatorRange
	idx := int(epoch % uint64(len(coordinatorParticipants)))
	if idx >= len(coordinatorParticipants) {
		return false, nil
	}
	return coordinatorParticipants[idx].ValidatorAddress == validatorAddress, nil
}

// GetCurrentTSSKeyId gets the current TSS key ID from the data provider.
func (c *Coordinator) GetCurrentTSSKeyId(ctx context.Context) (string, error) {
	return c.dataProvider.GetCurrentTSSKeyId(ctx)
}

// GetEligibleUV returns eligible validators for the given protocol type.
// Uses cached allValidators for performance.
func (c *Coordinator) GetEligibleUV(protocolType string) []*UniversalValidator {
	c.mu.RLock()
	allValidators := c.allValidators
	c.mu.RUnlock()

	if len(allValidators) == 0 {
		return nil
	}

	eligible := getParticipantsForProtocol(protocolType, allValidators)
	if eligible == nil {
		return nil
	}

	// Return a copy to prevent external modification
	result := make([]*UniversalValidator, len(eligible))
	copy(result, eligible)
	return result
}

// Start starts the coordinator loop.
func (c *Coordinator) Start(ctx context.Context) {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return
	}
	c.running = true
	c.mu.Unlock()

	c.logger.Info().Msg("starting coordinator")
	go c.pollLoop(ctx)
}

// Stop stops the coordinator loop.
func (c *Coordinator) Stop() {
	c.mu.Lock()
	if !c.running {
		c.mu.Unlock()
		return
	}
	c.running = false
	close(c.stopCh)
	c.mu.Unlock()

	c.logger.Info().Msg("stopping coordinator")
}

// pollLoop polls the database for pending events and processes them.
func (c *Coordinator) pollLoop(ctx context.Context) {
	ticker := time.NewTicker(c.pollInterval)
	defer ticker.Stop()

	// Update validators immediately on start
	c.updateValidators(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.stopCh:
			return
		case <-ticker.C:
			// Update validators at each polling interval
			c.updateValidators(ctx)
			if err := c.processPendingEvents(ctx); err != nil {
				c.logger.Error().Err(err).Msg("error processing pending events")
			}
		}
	}
}

// updateValidators fetches and caches all validators.
func (c *Coordinator) updateValidators(ctx context.Context) {
	allValidators, err := c.dataProvider.GetUniversalValidators(ctx)
	if err != nil {
		c.logger.Warn().Err(err).Msg("failed to update validators cache")
		return
	}

	c.mu.Lock()
	c.allValidators = allValidators
	c.mu.Unlock()

	c.logger.Debug().Int("count", len(allValidators)).Msg("updated validators cache")
}

// processPendingEvents checks if this node is coordinator, and only then reads DB and processes events.
func (c *Coordinator) processPendingEvents(ctx context.Context) error {
	currentBlock, err := c.dataProvider.GetLatestBlockNum(ctx)
	if err != nil {
		return errors.Wrap(err, "failed to get latest block number")
	}

	// Use cached validators (updated at polling interval)
	c.mu.RLock()
	allValidators := c.allValidators
	c.mu.RUnlock()

	if len(allValidators) == 0 {
		return nil // No validators, skip
	}

	// Check if we're coordinator for current block range
	// Get our own peerID from network (we need to find it from validators)
	var ourPeerID string
	for _, v := range allValidators {
		if v.ValidatorAddress == c.validatorAddress {
			ourPeerID = v.Network.PeerID
			break
		}
	}
	if ourPeerID == "" {
		return nil // Our validator not found, skip
	}

	isCoord, err := c.IsPeerCoordinator(ctx, ourPeerID)
	if err != nil {
		return errors.Wrap(err, "failed to check if we're coordinator")
	}
	if !isCoord {
		return nil // Not coordinator, do nothing
	}

	// We are coordinator - fetch and process events
	events, err := c.eventStore.GetPendingEvents(currentBlock, 10)
	if err != nil {
		return errors.Wrap(err, "failed to get pending events")
	}

	c.logger.Info().
		Int("count", len(events)).
		Uint64("current_block", currentBlock).
		Msg("found pending events")

	// Process each event: create setup message and send to all participants
	for _, event := range events {
		c.logger.Info().
			Str("event_id", event.EventID).
			Str("protocol_type", event.ProtocolType).
			Uint64("block_number", event.BlockNumber).
			Msg("processing event as coordinator")
		// Get participants based on protocol type (using cached allValidators)
		participants := getParticipantsForProtocol(event.ProtocolType, allValidators)
		if participants == nil {
			c.logger.Debug().Str("event_id", event.EventID).Str("protocol_type", event.ProtocolType).Msg("unknown protocol type")
			continue
		}
		if len(participants) == 0 {
			c.logger.Debug().Str("event_id", event.EventID).Msg("no participants for event")
			continue
		}

		if err := c.processEventAsCoordinator(ctx, event, participants); err != nil {
			c.logger.Error().
				Err(err).
				Str("event_id", event.EventID).
				Msg("failed to process event as coordinator")
		}
	}

	return nil
}

// processEventAsCoordinator processes a TSS event as the coordinator.
// Creates setup message based on event type and sends to all participants.
func (c *Coordinator) processEventAsCoordinator(ctx context.Context, event store.TSSEvent, participants []*UniversalValidator) error {
	// Sort participants by party ID for consistency
	sortedParticipants := make([]*UniversalValidator, len(participants))
	copy(sortedParticipants, participants)
	sort.Slice(sortedParticipants, func(i, j int) bool {
		return sortedParticipants[i].ValidatorAddress < sortedParticipants[j].ValidatorAddress
	})

	// Extract party IDs
	partyIDs := make([]string, len(sortedParticipants))
	for i, p := range sortedParticipants {
		partyIDs[i] = p.ValidatorAddress
	}

	// Calculate threshold
	threshold := CalculateThreshold(len(partyIDs))

	// Create setup message based on event type
	var setupData []byte
	var err error
	switch event.ProtocolType {
	case "keygen", "keyrefresh":
		// Keygen and keyrefresh use the same setup structure
		setupData, err = c.createKeygenSetup(threshold, partyIDs)
	case "quorumchange":
		setupData, err = c.createQcSetup(ctx, threshold, partyIDs, sortedParticipants)
	case "sign":
		setupData, err = c.createSignSetup(ctx, event.EventData, partyIDs)
	default:
		err = errors.Errorf("unknown protocol type: %s", event.ProtocolType)
	}

	if err != nil {
		return errors.Wrapf(err, "failed to create setup message for event %s", event.EventID)
	}

	// Create and send setup message to all participants
	setupMsg := Message{
		Type:         "setup",
		EventID:      event.EventID,
		Payload:      setupData,
		Participants: partyIDs,
	}
	setupMsgBytes, err := json.Marshal(setupMsg)
	if err != nil {
		return errors.Wrapf(err, "failed to marshal setup message for event %s", event.EventID)
	}

	// Initialize ACK tracking for this event
	c.ackMu.Lock()
	c.ackTracking[event.EventID] = &ackState{
		participants: partyIDs,
		ackedBy:      make(map[string]bool),
		ackCount:     0,
	}
	c.ackMu.Unlock()

	// Send to all participants via sendFn
	for _, p := range sortedParticipants {
		if err := c.send(ctx, p.Network.PeerID, setupMsgBytes); err != nil {
			c.logger.Warn().
				Err(err).
				Str("event_id", event.EventID).
				Str("receiver", p.ValidatorAddress).
				Msg("failed to send setup message")
			// Continue - other participants may still receive it
		} else {
			c.logger.Info().
				Str("event_id", event.EventID).
				Str("receiver", p.ValidatorAddress).
				Msg("sent setup message to participant")
		}
	}

	return nil
}

// HandleACK processes an ACK message from a participant.
// This is called by the session manager when coordinator receives an ACK.
func (c *Coordinator) HandleACK(ctx context.Context, senderPeerID string, eventID string) error {
	c.ackMu.Lock()
	defer c.ackMu.Unlock()

	state, exists := c.ackTracking[eventID]
	if !exists {
		// Not tracking this event, ignore (might be from a different coordinator)
		return nil
	}

	// Check if already ACKed
	if state.ackedBy[senderPeerID] {
		c.logger.Debug().
			Str("event_id", eventID).
			Str("sender", senderPeerID).
			Msg("duplicate ACK received, ignoring")
		return nil
	}

	// Verify sender is a participant
	senderPartyID, err := c.GetPartyIDFromPeerID(ctx, senderPeerID)
	if err != nil {
		return errors.Wrapf(err, "failed to get partyID for sender peerID %s", senderPeerID)
	}

	isParticipant := false
	for _, participantPartyID := range state.participants {
		if participantPartyID == senderPartyID {
			isParticipant = true
			break
		}
	}
	if !isParticipant {
		return errors.Errorf("sender %s (partyID: %s) is not a participant for event %s", senderPeerID, senderPartyID, eventID)
	}

	// Mark as ACKed
	state.ackedBy[senderPeerID] = true
	state.ackCount++

	c.logger.Debug().
		Str("event_id", eventID).
		Str("sender", senderPeerID).
		Str("sender_party_id", senderPartyID).
		Int("ack_count", state.ackCount).
		Int("expected_participants", len(state.participants)).
		Msg("coordinator received ACK")

	// Check if all participants have ACKed
	if state.ackCount == len(state.participants) {
		c.logger.Info().
			Str("event_id", eventID).
			Int("total_participants", len(state.participants)).
			Msg("all participants ACKed, coordinator will send BEGIN message")

		// Send BEGIN message to all participants
		beginMsg := Message{
			Type:         "begin",
			EventID:      eventID,
			Payload:      nil,
			Participants: state.participants,
		}
		beginMsgBytes, err := json.Marshal(beginMsg)
		if err != nil {
			return errors.Wrap(err, "failed to marshal begin message")
		}

		// Send to all participants
		for _, participantPartyID := range state.participants {
			participantPeerID, err := c.GetPeerIDFromPartyID(ctx, participantPartyID)
			if err != nil {
				c.logger.Warn().
					Err(err).
					Str("participant_party_id", participantPartyID).
					Msg("failed to get peerID for participant, skipping begin message")
				continue
			}

			if err := c.send(ctx, participantPeerID, beginMsgBytes); err != nil {
				c.logger.Warn().
					Err(err).
					Str("participant_peer_id", participantPeerID).
					Str("participant_party_id", participantPartyID).
					Msg("failed to send begin message to participant")
				continue
			}

			c.logger.Debug().
				Str("event_id", eventID).
				Str("participant_peer_id", participantPeerID).
				Msg("coordinator sent begin message to participant")
		}

		// Clean up ACK tracking after sending BEGIN
		delete(c.ackTracking, eventID)
	}

	return nil
}

// createKeygenSetup creates a keygen/keyrefresh setup message.
// Both keygen and keyrefresh use the same setup structure (keyID is nil - produces a new random keyId).
func (c *Coordinator) createKeygenSetup(threshold int, partyIDs []string) ([]byte, error) {
	// Encode participant IDs (separated by null bytes)
	participantIDs := make([]byte, 0, len(partyIDs)*10)
	for i, partyID := range partyIDs {
		if i > 0 {
			participantIDs = append(participantIDs, 0) // Separator
		}
		participantIDs = append(participantIDs, []byte(partyID)...)
	}

	setupData, err := session.DklsKeygenSetupMsgNew(threshold, nil, participantIDs)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create setup")
	}
	return setupData, nil
}

// createSignSetup creates a sign setup message.
// Requires loading the keyshare to extract keyID and messageHash from event data.
func (c *Coordinator) createSignSetup(ctx context.Context, eventData []byte, partyIDs []string) ([]byte, error) {
	// Get current TSS keyId from dataProvider
	keyIDStr, err := c.dataProvider.GetCurrentTSSKeyId(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get current TSS keyId")
	}

	// Load keyshare to ensure it exists (validation)
	keyshareBytes, err := c.keyshareManager.Get(keyIDStr)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to load keyshare for keyId %s", keyIDStr)
	}
	_ = keyshareBytes // Keyshare is loaded for validation, keyID is derived from string

	// Derive keyID bytes from string (SHA256 hash)
	keyIDBytes := deriveKeyIDBytes(keyIDStr)

	// Encode participant IDs (separated by null bytes)
	participantIDs := make([]byte, 0, len(partyIDs)*10)
	for i, partyID := range partyIDs {
		if i > 0 {
			participantIDs = append(participantIDs, 0) // Separator
		}
		participantIDs = append(participantIDs, []byte(partyID)...)
	}

	// Extract message string from eventData and hash it
	var message string
	// Try to parse as JSON first (in case eventData is JSON with "message" field)
	var eventDataJSON map[string]interface{}
	if err := json.Unmarshal(eventData, &eventDataJSON); err == nil {
		// Successfully parsed as JSON, try to get "message" field
		if msg, ok := eventDataJSON["message"].(string); ok {
			message = msg
		} else {
			return nil, errors.New("event data JSON does not contain 'message' string field")
		}
	} else {
		// Not JSON, treat eventData as the message string directly
		message = string(eventData)
	}

	if message == "" {
		return nil, errors.New("message is empty")
	}

	// Hash the message to get messageHash (SHA256)
	messageHash := sha256.Sum256([]byte(message))

	setupData, err := session.DklsSignSetupMsgNew(keyIDBytes, nil, messageHash[:], participantIDs)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create sign setup")
	}
	return setupData, nil
}

// createQcSetup creates a quorumchange setup message.
// Quorumchange changes the participant set of an existing key.
// oldParticipantIndices: indices of Active validators (staying participants)
// newParticipantIndices: indices of Pending Join validators (new participants)
func (c *Coordinator) createQcSetup(ctx context.Context, threshold int, partyIDs []string, participants []*UniversalValidator) ([]byte, error) {
	// Get current TSS keyId from dataProvider
	keyIDStr, err := c.dataProvider.GetCurrentTSSKeyId(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get current TSS keyId")
	}

	// Load old keyshare to get the key we're changing
	oldKeyshareBytes, err := c.keyshareManager.Get(keyIDStr)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to load keyshare for keyId %s", keyIDStr)
	}

	// Load keyshare handle from bytes
	oldKeyshareHandle, err := session.DklsKeyshareFromBytes(oldKeyshareBytes)
	if err != nil {
		return nil, errors.Wrap(err, "failed to load keyshare handle")
	}
	defer session.DklsKeyshareFree(oldKeyshareHandle)

	// Create a map of validator address to status for quick lookup
	validatorStatusMap := make(map[string]UVStatus)
	for _, v := range participants {
		validatorStatusMap[v.ValidatorAddress] = v.Status
	}

	// Calculate old participant indices (Active validators) and new participant indices (Pending Join validators)
	var oldParticipantIndices []int
	var newParticipantIndices []int

	for i, partyID := range partyIDs {
		status, exists := validatorStatusMap[partyID]
		if !exists {
			// Validator not found, skip
			continue
		}

		switch status {
		case UVStatusActive:
			// Active validators are old participants (staying)
			oldParticipantIndices = append(oldParticipantIndices, i)
		case UVStatusPendingJoin:
			// Pending Join validators are new participants (being added)
			newParticipantIndices = append(newParticipantIndices, i)
		}
	}

	setupData, err := session.DklsQcSetupMsgNew(oldKeyshareHandle, threshold, partyIDs, oldParticipantIndices, newParticipantIndices)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create quorumchange setup")
	}
	return setupData, nil
}

// getParticipantsForProtocol returns participants for a given protocol type.
// This is a centralized function to avoid duplication of participant selection logic.
func getParticipantsForProtocol(protocolType string, allValidators []*UniversalValidator) []*UniversalValidator {
	switch protocolType {
	case "keygen", "quorumchange":
		// For keygen and quorumchange: Active + Pending Join
		return getQuorumChangeParticipants(allValidators)
	case "keyrefresh":
		// For keyrefresh: Only Active
		return getActiveParticipants(allValidators)
	case "sign":
		// For sign: Random subset of >2/3 of (Active + Pending Leave)
		return getSignParticipants(allValidators)
	default:
		return nil
	}
}

// getCoordinatorParticipants returns validators eligible to be coordinators.
// Only Active validators can be coordinators.
// Special case: If there are no active validators (only pending join and 1 UV), that UV becomes coordinator.
func getCoordinatorParticipants(allValidators []*UniversalValidator) []*UniversalValidator {
	// First, get all active validators
	var active []*UniversalValidator
	for _, v := range allValidators {
		if v.Status == UVStatusActive {
			active = append(active, v)
		}
	}

	// If we have active validators, use them
	if len(active) > 0 {
		return active
	}

	// Special case: No active validators
	// If there's exactly 1 validator (pending join or any status), it becomes coordinator
	if len(allValidators) == 1 {
		return allValidators
	}

	// If no active and more than 1 validator, return empty (no coordinator)
	return nil
}

// getActiveParticipants returns only Active validators.
func getActiveParticipants(allValidators []*UniversalValidator) []*UniversalValidator {
	var participants []*UniversalValidator
	for _, v := range allValidators {
		if v.Status == UVStatusActive {
			participants = append(participants, v)
		}
	}
	return participants
}

// getQuorumChangeParticipants returns Active + Pending Join validators.
// Used for keygen and quorumchange protocols.
func getQuorumChangeParticipants(allValidators []*UniversalValidator) []*UniversalValidator {
	var participants []*UniversalValidator
	for _, v := range allValidators {
		if v.Status == UVStatusActive || v.Status == UVStatusPendingJoin {
			participants = append(participants, v)
		}
	}
	return participants
}

// getSignParticipants returns a random subset of >2/3 of (Active + Pending Leave) validators.
func getSignParticipants(allValidators []*UniversalValidator) []*UniversalValidator {
	// First, get all eligible validators (Active + Pending Leave)
	var eligible []*UniversalValidator
	for _, v := range allValidators {
		if v.Status == UVStatusActive || v.Status == UVStatusPendingLeave {
			eligible = append(eligible, v)
		}
	}

	// Use utils function to select random threshold subset
	return selectRandomThreshold(eligible)
}
