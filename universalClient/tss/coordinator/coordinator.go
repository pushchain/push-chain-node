package coordinator

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/crypto/secp256k1"
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

const (
	// PerChainCap is the max in-flight SIGN events per destination chain (default 16; below EVM mempool accountqueue 64).
	PerChainCap = 16
	// ConsecutiveWaitThreshold: after this many consecutive polls where a chain has in-flight events,
	// use finalized nonce to recover from stuck nonces (~200s at 10s poll).
	ConsecutiveWaitThreshold = 20
)

// ackState tracks ACK status for an event.
type ackState struct {
	participants []string
	ackedBy      map[string]bool // participant peerID -> has ACKed
	ackCount     int
}

// Coordinator handles coordinator logic for TSS events.
type Coordinator struct {
	// Dependencies
	eventStore      *eventstore.Store
	pushCore        *pushcore.Client
	keyshareManager *keyshare.Manager
	chains          *chains.Chains

	// Config
	validatorAddress string
	coordinatorRange uint64
	pollInterval     time.Duration
	logger           zerolog.Logger
	send             SendFunc

	// Lifecycle and cache
	mu            sync.RWMutex
	running       bool
	stopCh        chan struct{}
	allValidators []*types.UniversalValidator

	// ACK tracking for events we're coordinating (even if not participant)
	ackTracking map[string]*ackState
	ackMu       sync.RWMutex

	// chainWaitMu guards consecutiveWaitPerChain (stuck nonce recovery).
	chainWaitMu             sync.Mutex
	consecutiveWaitPerChain map[string]int
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
		eventStore:              eventStore,
		pushCore:                pushCore,
		keyshareManager:         keyshareManager,
		chains:                  chains,
		validatorAddress:        validatorAddress,
		coordinatorRange:        coordinatorRange,
		pollInterval:            pollInterval,
		logger:                  logger,
		send:                    send,
		stopCh:                  make(chan struct{}),
		ackTracking:             make(map[string]*ackState),
		consecutiveWaitPerChain: make(map[string]int),
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

// IsPeerCoordinator reports whether the given peerID is the coordinator for the current block.
// Used by the session manager to validate that an incoming setup message comes from the coordinator.
func (c *Coordinator) IsPeerCoordinator(ctx context.Context, peerID string) (bool, error) {
	currentBlock, err := c.pushCore.GetLatestBlock(ctx)
	if err != nil {
		return false, errors.Wrap(err, "failed to get latest block")
	}

	c.mu.RLock()
	allValidators := c.allValidators
	c.mu.RUnlock()

	if len(allValidators) == 0 {
		return false, nil
	}

	// Resolve peerID → validator address.
	var validatorAddress string
	for _, v := range allValidators {
		if v.NetworkInfo != nil && v.NetworkInfo.PeerId == peerID {
			if v.IdentifyInfo != nil {
				validatorAddress = v.IdentifyInfo.CoreValidatorAddress
			}
			break
		}
	}
	if validatorAddress == "" {
		return false, nil // Peer not in known validators
	}

	return coordinatorAddressForBlock(allValidators, c.coordinatorRange, currentBlock) == validatorAddress, nil
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

// GetTSSAddress returns the TSS ECDSA address derived from the current TSS public key (compressed secp256k1).
func (c *Coordinator) GetTSSAddress(ctx context.Context) (string, error) {
	key, err := c.pushCore.GetCurrentKey(ctx)
	if err != nil {
		return "", errors.Wrap(err, "failed to get current TSS key")
	}
	if key == nil || key.TssPubkey == "" {
		return "", errors.New("no TSS key found")
	}
	pubkeyHex := strings.TrimPrefix(strings.TrimSpace(key.TssPubkey), "0x")
	pubkeyBytes, err := hex.DecodeString(pubkeyHex)
	if err != nil {
		return "", errors.Wrap(err, "failed to decode TSS public key")
	}
	if len(pubkeyBytes) != 33 {
		return "", errors.Errorf("invalid TSS public key length: %d bytes (expected 33)", len(pubkeyBytes))
	}
	vkX, vkY := secp256k1.DecompressPubkey(pubkeyBytes)
	if vkX == nil || vkY == nil {
		return "", errors.New("failed to decompress TSS public key")
	}
	xBytes := vkX.FillBytes(make([]byte, 32))
	yBytes := vkY.FillBytes(make([]byte, 32))
	addressBytes := crypto.Keccak256(append(xBytes, yBytes...))[12:]
	return "0x" + hex.EncodeToString(addressBytes), nil
}

// GetEligibleUV returns ALL eligible validators for the given protocol type (no random selection).
// Used by the session manager to check whether a setup-message sender is eligible to participate.
// For SIGN coordinator setup the coordinator calls getSignParticipants (random threshold subset).
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
			if err := c.processConfirmedEvents(ctx); err != nil {
				c.logger.Error().Err(err).Msg("error processing confirmed events")
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

// processConfirmedEvents checks if this node is the coordinator for the current block range and,
// if so, fetches CONFIRMED events from the database and drives them through TSS setup.
// Called on every poll tick; returns early (no-op) when this node is not coordinator.
func (c *Coordinator) processConfirmedEvents(ctx context.Context) error {
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

	// Check if this node is the coordinator for the current block range.
	// Use coordinatorAddressForBlock directly so we don't make a second GetLatestBlock RPC call.
	if coordinatorAddressForBlock(allValidators, c.coordinatorRange, currentBlock) != c.validatorAddress {
		c.logger.Debug().Msg("processConfirmedEvents: not coordinator, skipping")
		return nil
	}

	c.logger.Info().Msg("processConfirmedEvents: we ARE coordinator, processing events")

	events, err := c.eventStore.GetNonExpiredConfirmedEvents(currentBlock, 10, 0)
	if err != nil {
		return errors.Wrap(err, "failed to get confirmed events")
	}

	inFlightPerChain, err := c.getInFlightSignCountPerChain()
	if err != nil {
		return errors.Wrap(err, "failed to get in-flight sign count per chain")
	}

	c.logger.Info().
		Int("count", len(events)).
		Uint64("current_block", currentBlock).
		Msg("found confirmed events")

	// Per-chain nonce cache: fetched once per chain per poll, then incremented locally (n, n+1, n+2, …).
	nonceByChain := make(map[string]uint64)
	skippedChains := make(map[string]bool)

	for _, event := range events {
		var assignedNonce *uint64
		if event.Type == string(ProtocolSign) {
			chain := extractDestinationChain(event.EventData)
			if chain == "" {
				continue
			}

			nonce, ok := c.assignSignNonce(ctx, event, chain, inFlightPerChain, nonceByChain, skippedChains)
			if !ok {
				continue
			}
			assignedNonce = &nonce
		}

		c.logger.Info().
			Str("event_id", event.EventID).
			Str("type", event.Type).
			Uint64("block_height", event.BlockHeight).
			Msg("processing event as coordinator")
		// For SIGN: pick a random threshold subset (>2/3 of eligible) rather than all eligible.
		// A threshold subset suffices for signing and is more resilient when some nodes are offline.
		// For all other protocols (keygen, keyrefresh, quorum_change), all eligible must participate.
		var participants []*types.UniversalValidator
		if event.Type == string(ProtocolSign) {
			participants = getSignParticipants(allValidators)
		} else {
			participants = getEligibleForProtocol(event.Type, allValidators)
		}
		if participants == nil {
			c.logger.Debug().Str("event_id", event.EventID).Str("type", event.Type).Msg("unknown protocol type")
			continue
		}
		if len(participants) == 0 {
			c.logger.Debug().Str("event_id", event.EventID).Msg("no participants for event")
			continue
		}

		if err := c.processEventAsCoordinator(ctx, event, participants, assignedNonce); err != nil {
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
// assignedNonce is set only for SIGN events; nil for keygen/keyrefresh/quorumchange.
func (c *Coordinator) processEventAsCoordinator(ctx context.Context, event store.Event, participants []*types.UniversalValidator, assignedNonce *uint64) error {
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
		setupData, unsignedTxReq, err = c.createSignSetup(ctx, event.EventData, partyIDs, assignedNonce)
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
// assignedNonce must be set for SIGN (coordinator-assigned nonce); participants use it for verification.
// Returns the setup data, unsigned transaction request (for participant verification), and error.
func (c *Coordinator) createSignSetup(ctx context.Context, eventData []byte, partyIDs []string, assignedNonce *uint64) ([]byte, *common.UnSignedOutboundTxReq, error) {
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

	// Build the transaction and get signing parameters (use coordinator-assigned nonce when provided)
	signingReq, err := c.buildSignTransaction(ctx, eventData, assignedNonce)
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
func (c *Coordinator) buildSignTransaction(ctx context.Context, eventData []byte, assignedNonce *uint64) (*common.UnSignedOutboundTxReq, error) {
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

	// Get the signing request with the gas price from oracle (nonce is required for SIGN)
	if assignedNonce == nil {
		return nil, errors.New("assigned nonce is required for sign transaction")
	}
	signingReq, err := builder.GetOutboundSigningRequest(ctx, &data, gasPrice, *assignedNonce)
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

// getEligibleForProtocol returns ALL eligible validators for a protocol type (no random selection).
// Used by the session manager for eligibility validation and by the coordinator for keygen,
// keyrefresh, and quorum_change setup (all eligible validators must participate in those protocols).
// For SIGN coordinator setup, use getSignParticipants instead (random threshold subset).
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

// coordinatorAddressForBlock returns the validator address of the coordinator for the given block.
// Coordinator rotation: epoch = currentBlock / coordinatorRange; coordinator = active[epoch % len(active)].
// Falls back to all validators when no Active validators exist (bootstrap / single-node case).
// Pure function — no I/O, deterministic, safe to call from any context.
func coordinatorAddressForBlock(allValidators []*types.UniversalValidator, coordinatorRange uint64, currentBlock uint64) string {
	coordinatorParticipants := getCoordinatorParticipants(allValidators)
	if len(coordinatorParticipants) == 0 {
		return ""
	}
	epoch := currentBlock / coordinatorRange
	idx := int(epoch % uint64(len(coordinatorParticipants)))
	if coordinatorParticipants[idx].IdentifyInfo != nil {
		return coordinatorParticipants[idx].IdentifyInfo.CoreValidatorAddress
	}
	return ""
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

// getInFlightSignCountPerChain returns per-chain in-flight SIGN count.
func (c *Coordinator) getInFlightSignCountPerChain() (map[string]int, error) {
	inFlight, err := c.eventStore.GetInFlightSignEvents()
	if err != nil {
		return nil, err
	}
	perChain := make(map[string]int)
	for _, event := range inFlight {
		chain := extractDestinationChain(event.EventData)
		if chain != "" {
			perChain[chain]++
		}
	}
	return perChain, nil
}

// assignSignNonce resolves the nonce for a SIGN event on the given destination chain.
// Returns (nonce, true) if the event should proceed, or (0, false) to skip.
//
// Nonce is fetched from chain once per destination chain per poll, then incremented
// locally for each additional event (n, n+1, n+2, …).
func (c *Coordinator) assignSignNonce(
	ctx context.Context,
	event store.Event,
	chain string,
	inFlightPerChain map[string]int,
	nonceByChain map[string]uint64,
	skippedChains map[string]bool,
) (uint64, bool) {
	if skippedChains[chain] {
		return 0, false
	}

	// ── Subsequent event for this chain (nonce already fetched this poll) ──
	if _, exists := nonceByChain[chain]; exists {
		if inFlightPerChain[chain] >= PerChainCap {
			return 0, false
		}
		nonceByChain[chain]++
		inFlightPerChain[chain]++
		return nonceByChain[chain], true
	}

	// ── First event for this chain this poll ──
	// Decide: process normally, wait (skip), or recover with finalized nonce.
	useFinalized := false

	if inFlightPerChain[chain] > 0 {
		c.chainWaitMu.Lock()
		consecutiveWait := c.consecutiveWaitPerChain[chain]
		c.chainWaitMu.Unlock()

		if consecutiveWait < ConsecutiveWaitThreshold {
			// Still within patience — skip chain, let in-flight events clear
			c.chainWaitMu.Lock()
			c.consecutiveWaitPerChain[chain]++
			c.chainWaitMu.Unlock()
			skippedChains[chain] = true
			c.logger.Debug().
				Str("chain", chain).
				Int("in_flight", inFlightPerChain[chain]).
				Int("consecutive_wait", consecutiveWait+1).
				Msg("skipping chain — waiting for in-flight to clear")
			return 0, false
		}

		// Patience exhausted — recover with finalized nonce.
		// Cap is intentionally bypassed: stuck events have stale nonces and will
		// be cleared by broadcaster → resolver → REVERTED.
		useFinalized = true
		c.logger.Info().
			Str("chain", chain).
			Int("in_flight", inFlightPerChain[chain]).
			Int("consecutive_wait", consecutiveWait).
			Msg("patience exhausted — recovering with finalized nonce")
	}

	// Fetch nonce (once per chain per poll)
	nonce, err := c.getNextNonceForChain(ctx, chain, useFinalized)
	if err != nil {
		c.logger.Error().Err(err).Str("chain", chain).Str("event_id", event.EventID).
			Msg("failed to get next nonce")
		return 0, false
	}

	nonceByChain[chain] = nonce
	inFlightPerChain[chain]++

	c.chainWaitMu.Lock()
	c.consecutiveWaitPerChain[chain] = 0
	c.chainWaitMu.Unlock()

	return nonce, true
}

// getNextNonceForChain queries the chain for the next nonce to assign.
// useFinalized: when true, uses finalized nonce (stuck nonce recovery); otherwise uses pending.
func (c *Coordinator) getNextNonceForChain(ctx context.Context, chain string, useFinalized bool) (uint64, error) {
	if c.chains == nil {
		return 0, errors.New("chains manager not configured")
	}
	client, err := c.chains.GetClient(chain)
	if err != nil {
		return 0, err
	}
	builder, err := client.GetTxBuilder()
	if err != nil {
		return 0, err
	}
	tssAddress, err := c.GetTSSAddress(ctx)
	if err != nil {
		return 0, err
	}
	return builder.GetNextNonce(ctx, tssAddress, useFinalized)
}

// extractDestinationChain extracts the destination_chain field from event data JSON.
func extractDestinationChain(eventData []byte) string {
	if len(eventData) == 0 {
		return ""
	}
	var data struct {
		DestinationChain string `json:"destination_chain"`
	}
	if err := json.Unmarshal(eventData, &data); err != nil {
		return ""
	}
	return data.DestinationChain
}
