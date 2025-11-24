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

	coordinatorParticipants := getKeygenKeyrefreshParticipants(allValidators)
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

	var eligible []*UniversalValidator
	switch protocolType {
	case "keygen", "keyrefresh":
		// For keygen and keyrefresh: Active + Pending Join
		eligible = getKeygenKeyrefreshParticipants(allValidators)
	case "sign":
		// For sign: Random subset of >2/3 of (Active + Pending Leave)
		eligible = getSignParticipants(allValidators)
	default:
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

	// Process each event: create setup message and send to all participants
	for _, event := range events {
		// Get participants based on protocol type (using cached allValidators)
		var participants []*UniversalValidator
		switch event.ProtocolType {
		case "keygen", "keyrefresh":
			participants = getKeygenKeyrefreshParticipants(allValidators)
		case "sign":
			participants = getSignParticipants(allValidators)
		default:
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

// getKeygenKeyrefreshParticipants returns Active + Pending Join validators.
func getKeygenKeyrefreshParticipants(allValidators []*UniversalValidator) []*UniversalValidator {
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
