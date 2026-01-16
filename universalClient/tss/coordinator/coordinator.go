package coordinator

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"sort"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/pkg/errors"
	"github.com/rs/zerolog"

	session "go-wrapper/go-dkls/sessions"

	"github.com/pushchain/push-chain-node/universalClient/chains"
	"github.com/pushchain/push-chain-node/universalClient/chains/common"
	"github.com/pushchain/push-chain-node/universalClient/pushcore"
	"github.com/pushchain/push-chain-node/universalClient/store"
	"github.com/pushchain/push-chain-node/universalClient/tss/eventstore"
	"github.com/pushchain/push-chain-node/universalClient/tss/keyshare"
	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
	"github.com/pushchain/push-chain-node/x/uvalidator/types"
)

// Coordinator handles coordinator logic for TSS events.
type Coordinator struct {
	eventStore       *eventstore.Store
	pushCore         *pushcore.Client
	keyshareManager  *keyshare.Manager
	chains           *chains.Chains
	validatorAddress string
	coordinatorRange uint64
	pollInterval     time.Duration
	logger           zerolog.Logger
	send             SendFunc

	// Internal state
	mu            sync.RWMutex
	running       bool
	stopCh        chan struct{}
	allValidators []*types.UniversalValidator // Cached validators, updated at polling interval

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
	pushCore *pushcore.Client,
	keyshareManager *keyshare.Manager,
	chains *chains.Chains,
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
		pushCore:         pushCore,
		keyshareManager:  keyshareManager,
		chains:           chains,
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
		if v.NetworkInfo != nil && v.NetworkInfo.PeerId == peerID {
			if v.IdentifyInfo != nil {
				return v.IdentifyInfo.CoreValidatorAddress, nil
			}
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
		if v.IdentifyInfo != nil && v.IdentifyInfo.CoreValidatorAddress == partyID {
			if v.NetworkInfo != nil {
				return v.NetworkInfo.PeerId, nil
			}
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
		if v.NetworkInfo != nil && v.NetworkInfo.PeerId == peerID {
			return v.NetworkInfo.MultiAddrs, nil
		}
	}

	return nil, errors.Errorf("peerID %s not found in validators", peerID)
}

// GetLatestBlockNum gets the latest block number from pushCore.
func (c *Coordinator) GetLatestBlockNum(ctx context.Context) (uint64, error) {
	return c.pushCore.GetLatestBlock(ctx)
}

// IsPeerCoordinator checks if the given peerID is the coordinator for the current block.
// Uses cached allValidators for performance.
func (c *Coordinator) IsPeerCoordinator(ctx context.Context, peerID string) (bool, error) {
	currentBlock, err := c.pushCore.GetLatestBlock(ctx)
	if err != nil {
		return false, errors.Wrap(err, "failed to get latest block")
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
		if v.NetworkInfo != nil && v.NetworkInfo.PeerId == peerID {
			if v.IdentifyInfo != nil {
				validatorAddress = v.IdentifyInfo.CoreValidatorAddress
				break
			}
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
	coordValidatorAddr := ""
	if coordinatorParticipants[idx].IdentifyInfo != nil {
		coordValidatorAddr = coordinatorParticipants[idx].IdentifyInfo.CoreValidatorAddress
	}
	return coordValidatorAddr == validatorAddress, nil
}

// GetCurrentTSSKey gets the current TSS key ID and public key from pushCore.
func (c *Coordinator) GetCurrentTSSKey(ctx context.Context) (string, string, error) {
	key, err := c.pushCore.GetCurrentKey(ctx)
	if err != nil {
		return "", "", err
	}
	if key == nil {
		return "", "", nil // No key exists
	}
	return key.KeyId, key.TssPubkey, nil
}

// getTSSAddress gets the TSS ECDSA address from the current TSS public key
// The TSS address is always the same ECDSA address derived from the TSS public key
func (c *Coordinator) getTSSAddress(ctx context.Context) (string, error) {
	key, err := c.pushCore.GetCurrentKey(ctx)
	if err != nil {
		return "", errors.Wrap(err, "failed to get current TSS key")
	}
	if key == nil || key.TssPubkey == "" {
		return "", errors.New("no TSS key found")
	}

	// Derive ECDSA address from public key
	// TSS public key is hex-encoded, uncompressed format (0x04 prefix + 64 bytes)
	pubkeyBytes, err := hex.DecodeString(key.TssPubkey)
	if err != nil {
		return "", errors.Wrap(err, "failed to decode TSS public key")
	}

	// Skip 0x04 prefix (first byte) and hash the rest
	if len(pubkeyBytes) < 65 {
		return "", errors.New("invalid TSS public key length")
	}
	pubkeyBytes = pubkeyBytes[1:] // Remove 0x04 prefix

	// Hash with keccak256 and take last 20 bytes
	addressBytes := crypto.Keccak256(pubkeyBytes)[12:]

	return "0x" + hex.EncodeToString(addressBytes), nil
}

// GetEligibleUV returns eligible validators for the given protocol type.
// Uses cached allValidators for performance.
// For sign: returns ALL eligible validators (Active + Pending Leave), not a random subset.
// This is used for validation - the random subset selection happens only in processEventAsCoordinator.
func (c *Coordinator) GetEligibleUV(protocolType string) []*types.UniversalValidator {
	c.mu.RLock()
	allValidators := c.allValidators
	c.mu.RUnlock()

	if len(allValidators) == 0 {
		return nil
	}

	eligible := getEligibleForProtocol(protocolType, allValidators)
	if eligible == nil {
		return nil
	}

	// Return a copy to prevent external modification
	result := make([]*types.UniversalValidator, len(eligible))
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
	allValidators, err := c.pushCore.GetAllUniversalValidators(ctx)
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
	currentBlock, err := c.pushCore.GetLatestBlock(ctx)
	if err != nil {
		return errors.Wrap(err, "failed to get latest block")
	}

	// Use cached validators (updated at polling interval)
	c.mu.RLock()
	allValidators := c.allValidators
	c.mu.RUnlock()

	if len(allValidators) == 0 {
		return nil // No validators, skip
	}

	c.logger.Debug().
		Int("total_validators", len(allValidators)).
		Msg("processPendingEvents: checking coordinator")

	// Check if we're coordinator for current block range
	// Get our own peerID from network (we need to find it from validators)
	var ourPeerID string
	for _, v := range allValidators {
		if v.IdentifyInfo != nil && v.IdentifyInfo.CoreValidatorAddress == c.validatorAddress {
			if v.NetworkInfo != nil {
				ourPeerID = v.NetworkInfo.PeerId
			}
			break
		}
	}
	if ourPeerID == "" {
		c.logger.Debug().
			Str("validator_address", c.validatorAddress).
			Msg("processPendingEvents: our validator not found in validators list")
		return nil // Our validator not found, skip
	}

	isCoord, err := c.IsPeerCoordinator(ctx, ourPeerID)
	if err != nil {
		return errors.Wrap(err, "failed to check if we're coordinator")
	}
	if !isCoord {
		c.logger.Debug().Msg("processPendingEvents: we are NOT coordinator, skipping")
		return nil // Not coordinator, do nothing
	}

	c.logger.Info().Msg("processPendingEvents: we ARE coordinator, processing events")

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
			Str("type", event.Type).
			Uint64("block_height", event.BlockHeight).
			Msg("processing event as coordinator")
		// Get participants based on protocol type (using cached allValidators)
		participants := getParticipantsForProtocol(event.Type, allValidators)
		if participants == nil {
			c.logger.Debug().Str("event_id", event.EventID).Str("type", event.Type).Msg("unknown protocol type")
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
func (c *Coordinator) processEventAsCoordinator(ctx context.Context, event store.Event, participants []*types.UniversalValidator) error {
	// Sort participants by party ID for consistency
	sortedParticipants := make([]*types.UniversalValidator, len(participants))
	copy(sortedParticipants, participants)
	sort.Slice(sortedParticipants, func(i, j int) bool {
		addrI := ""
		addrJ := ""
		if sortedParticipants[i].IdentifyInfo != nil {
			addrI = sortedParticipants[i].IdentifyInfo.CoreValidatorAddress
		}
		if sortedParticipants[j].IdentifyInfo != nil {
			addrJ = sortedParticipants[j].IdentifyInfo.CoreValidatorAddress
		}
		return addrI < addrJ
	})

	// Extract party IDs
	partyIDs := make([]string, len(sortedParticipants))
	for i, p := range sortedParticipants {
		if p.IdentifyInfo != nil {
			partyIDs[i] = p.IdentifyInfo.CoreValidatorAddress
		}
	}

	// Calculate threshold
	threshold := CalculateThreshold(len(partyIDs))

	// Create setup message based on event type
	var setupData []byte
	var unsignedTxReq *common.UnSignedOutboundTxReq
	var err error
	switch event.Type {
	case string(ProtocolKeygen), string(ProtocolKeyrefresh):
		// Keygen and keyrefresh use the same setup structure
		setupData, err = c.createKeygenSetup(threshold, partyIDs)
	case string(ProtocolQuorumChange):
		setupData, err = c.createQcSetup(ctx, threshold, partyIDs, sortedParticipants)
	case string(ProtocolSign):
		setupData, unsignedTxReq, err = c.createSignSetup(ctx, event.EventData, partyIDs)
	default:
		err = errors.Errorf("unknown protocol type: %s", event.Type)
	}

	if err != nil {
		return errors.Wrapf(err, "failed to create setup message for event %s", event.EventID)
	}

	// Create and send setup message to all participants
	setupMsg := Message{
		Type:                  "setup",
		EventID:               event.EventID,
		Payload:               setupData,
		Participants:          partyIDs,
		UnSignedOutboundTxReq: unsignedTxReq, // nil for non-sign events
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
		if p.NetworkInfo == nil {
			continue
		}
		receiverAddr := ""
		if p.IdentifyInfo != nil {
			receiverAddr = p.IdentifyInfo.CoreValidatorAddress
		}
		if err := c.send(ctx, p.NetworkInfo.PeerId, setupMsgBytes); err != nil {
			c.logger.Warn().
				Err(err).
				Str("event_id", event.EventID).
				Str("receiver", receiverAddr).
				Msg("failed to send setup message")
			// Continue - other participants may still receive it
		} else {
			c.logger.Info().
				Str("event_id", event.EventID).
				Str("receiver", receiverAddr).
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

// createSignSetup creates a sign setup message and returns the unsigned transaction request.
// Uses the OutboundTxBuilder to build the actual transaction for the destination chain.
// Returns the setup data, unsigned transaction request (for participant verification), and error.
func (c *Coordinator) createSignSetup(ctx context.Context, eventData []byte, partyIDs []string) ([]byte, *common.UnSignedOutboundTxReq, error) {
	// Get current TSS keyId from pushCore
	key, err := c.pushCore.GetCurrentKey(ctx)
	if err != nil {
		return nil, nil, errors.Wrap(err, "failed to get current TSS keyId")
	}
	if key == nil {
		return nil, nil, errors.New("no TSS key exists")
	}
	keyIDStr := key.KeyId

	// Load keyshare to ensure it exists (validation)
	keyshareBytes, err := c.keyshareManager.Get(keyIDStr)
	if err != nil {
		return nil, nil, errors.Wrapf(err, "failed to load keyshare for keyId %s", keyIDStr)
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

	// Build the transaction and get signing parameters
	signingReq, err := c.buildSignTransaction(ctx, eventData)
	if err != nil {
		return nil, nil, errors.Wrap(err, "failed to build sign transaction")
	}

	setupData, err := session.DklsSignSetupMsgNew(keyIDBytes, nil, signingReq.SigningHash, participantIDs)
	if err != nil {
		return nil, nil, errors.Wrap(err, "failed to create sign setup")
	}

	return setupData, signingReq, nil
}

// buildSignTransaction builds the outbound transaction using the appropriate OutboundTxBuilder.
func (c *Coordinator) buildSignTransaction(ctx context.Context, eventData []byte) (*common.UnSignedOutboundTxReq, error) {
	if len(eventData) == 0 {
		return nil, errors.New("event data is empty")
	}

	var data uexecutortypes.OutboundCreatedEvent
	if err := json.Unmarshal(eventData, &data); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal outbound event data")
	}

	if data.TxID == "" {
		return nil, errors.New("outbound event missing tx_id")
	}

	if data.DestinationChain == "" {
		return nil, errors.New("outbound event missing destination_chain")
	}

	if c.chains == nil {
		return nil, errors.New("chains manager not configured")
	}

	// Get gas price from pushcore oracle
	gasPrice, err := c.pushCore.GetGasPrice(ctx, data.DestinationChain)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get gas price for chain %s", data.DestinationChain)
	}

	// Get the client for the destination chain
	client, err := c.chains.GetClient(data.DestinationChain)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get client for chain %s", data.DestinationChain)
	}

	// Get the builder from the client
	builder, err := client.GetTxBuilder()
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get tx builder for chain %s", data.DestinationChain)
	}

	// Get TSS ECDSA address (same for all chains)
	tssAddress, err := c.getTSSAddress(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get TSS address")
	}

	// Get the signing request with the gas price from oracle
	signingReq, err := builder.GetOutboundSigningRequest(ctx, &data, gasPrice, tssAddress)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get outbound signing request")
	}

	return signingReq, nil
}

// createQcSetup creates a quorumchange setup message.
// Quorumchange changes the participant set of an existing key.
// oldParticipantIndices: indices of Active validators (staying participants)
// newParticipantIndices: indices of Pending Join validators (new participants)
// @dev - For Qc to be successfull, oldPartcipants must form the quorum ie > 2/3 of the old participants.
// @dev - Although tss lib can also take pending leave participants in oldParticipantIndices, we don't use that since it needs to be considered that old participants are gone and will only result in errors.
func (c *Coordinator) createQcSetup(ctx context.Context, threshold int, partyIDs []string, participants []*types.UniversalValidator) ([]byte, error) {
	// Get current TSS keyId from pushCore
	key, err := c.pushCore.GetCurrentKey(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get current TSS keyId")
	}
	if key == nil {
		return nil, errors.New("no TSS key exists")
	}
	keyIDStr := key.KeyId

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
	validatorStatusMap := make(map[string]types.UVStatus)
	for _, v := range participants {
		if v.IdentifyInfo != nil && v.LifecycleInfo != nil {
			validatorStatusMap[v.IdentifyInfo.CoreValidatorAddress] = v.LifecycleInfo.CurrentStatus
		}
	}

	// Calculate old participant indices (Active validators)
	// newParticipantIndices should include all parties in the new quorum (all partyIDs)
	var oldParticipantIndices []int
	var newParticipantIndices []int

	// oldParticipantIndices: indices of Active validators (staying participants)
	for i, partyID := range partyIDs {
		status, exists := validatorStatusMap[partyID]
		if !exists {
			// Validator not found, skip
			continue
		}

		if status == types.UVStatus_UV_STATUS_ACTIVE {
			// Active validators are old participants (staying)
			oldParticipantIndices = append(oldParticipantIndices, i)
		}
	}

	// newParticipantIndices: all parties in the new quorum (all partyIDs)
	for i := range partyIDs {
		newParticipantIndices = append(newParticipantIndices, i)
	}

	setupData, err := session.DklsQcSetupMsgNew(oldKeyshareHandle, threshold, partyIDs, oldParticipantIndices, newParticipantIndices)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create quorumchange setup")
	}
	return setupData, nil
}

// getEligibleForProtocol returns all eligible validators for a given protocol type.
// This is used for validation - returns ALL eligible validators, not a random subset.
// For sign: returns all (Active + Pending Leave) validators.
func getEligibleForProtocol(protocolType string, allValidators []*types.UniversalValidator) []*types.UniversalValidator {
	switch protocolType {
	case string(ProtocolKeygen), string(ProtocolQuorumChange):
		// Active + Pending Join
		return getQuorumChangeParticipants(allValidators)
	case string(ProtocolKeyrefresh):
		// Active + Pending Leave
		return getSignEligible(allValidators)
	case string(ProtocolSign):
		// Active + Pending Leave
		return getSignEligible(allValidators)
	default:
		return nil
	}
}

// getParticipantsForProtocol returns participants for a given protocol type.
// This is a centralized function to avoid duplication of participant selection logic.
// For sign: returns a random subset (used by coordinator when creating setup).
// For other protocols: returns all eligible participants (same as getEligibleForProtocol).
func getParticipantsForProtocol(protocolType string, allValidators []*types.UniversalValidator) []*types.UniversalValidator {
	// For sign, we need random subset; for others, same as eligible
	if protocolType == string(ProtocolSign) {
		return getSignParticipants(allValidators)
	}
	// For other protocols, return all eligible (same logic)
	return getEligibleForProtocol(protocolType, allValidators)
}

// getCoordinatorParticipants returns validators eligible to be coordinators.
// Only Active validators can be coordinators.
// Special case: If there are no active validators (only pending join and 1 UV), that UV becomes coordinator.
func getCoordinatorParticipants(allValidators []*types.UniversalValidator) []*types.UniversalValidator {
	// Get all active validators (reuse existing function)
	active := getActiveParticipants(allValidators)

	// If we have active validators, use them
	if len(active) > 0 {
		return active
	}

	// Special case: No active validators
	return allValidators
}

// getActiveParticipants returns only Active validators.
func getActiveParticipants(allValidators []*types.UniversalValidator) []*types.UniversalValidator {
	var participants []*types.UniversalValidator
	for _, v := range allValidators {
		if v.LifecycleInfo != nil && v.LifecycleInfo.CurrentStatus == types.UVStatus_UV_STATUS_ACTIVE {
			participants = append(participants, v)
		}
	}
	return participants
}

// getQuorumChangeParticipants returns Active + Pending Join validators.
// Used for keygen and quorumchange protocols.
func getQuorumChangeParticipants(allValidators []*types.UniversalValidator) []*types.UniversalValidator {
	var participants []*types.UniversalValidator
	for _, v := range allValidators {
		if v.LifecycleInfo != nil {
			status := v.LifecycleInfo.CurrentStatus
			if status == types.UVStatus_UV_STATUS_ACTIVE || status == types.UVStatus_UV_STATUS_PENDING_JOIN {
				participants = append(participants, v)
			}
		}
	}
	return participants
}

// getSignEligible returns ALL eligible validators for sign protocol (Active + Pending Leave).
// This is used for validation - returns all eligible validators without random selection.
func getSignEligible(allValidators []*types.UniversalValidator) []*types.UniversalValidator {
	var eligible []*types.UniversalValidator
	for _, v := range allValidators {
		if v.LifecycleInfo != nil {
			status := v.LifecycleInfo.CurrentStatus
			if status == types.UVStatus_UV_STATUS_ACTIVE || status == types.UVStatus_UV_STATUS_PENDING_LEAVE {
				eligible = append(eligible, v)
			}
		}
	}
	return eligible
}

// getSignParticipants returns a random subset of >2/3 of (Active + Pending Leave) validators.
// This is used by the coordinator when creating setup messages.
func getSignParticipants(allValidators []*types.UniversalValidator) []*types.UniversalValidator {
	// First, get all eligible validators (Active + Pending Leave)
	eligible := getSignEligible(allValidators)

	// Use utils function to select random threshold subset
	return selectRandomThreshold(eligible)
}
