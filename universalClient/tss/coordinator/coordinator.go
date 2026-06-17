package coordinator

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/crypto/secp256k1"
	"github.com/rs/zerolog"

	session "go-wrapper/go-dkls/sessions"

	"github.com/pushchain/push-chain-node/universalClient/chains"
	"github.com/pushchain/push-chain-node/universalClient/chains/common"
	"github.com/pushchain/push-chain-node/universalClient/store"
	"github.com/pushchain/push-chain-node/universalClient/tss/eventstore"
	"github.com/pushchain/push-chain-node/universalClient/tss/keyshare"
	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
	utsstypes "github.com/pushchain/push-chain-node/x/utss/types"
	"github.com/pushchain/push-chain-node/x/uvalidator/types"
)

// PushCoreClient is the subset of pushcore.Client the coordinator depends on.
// Defined as an interface so tests can inject a mock without spinning up a
// real Push Chain RPC endpoint. *pushcore.Client satisfies this interface.
type PushCoreClient interface {
	GetLatestBlock(ctx context.Context) (uint64, error)
	GetCurrentKey(ctx context.Context) (*utsstypes.TssKey, error)
	GetAllUniversalValidators(ctx context.Context) ([]*types.UniversalValidator, error)
}

const (
	// PerChainCap is the max in-flight SIGN events per destination chain
	// (default 16; below EVM mempool accountqueue 64).
	// EVM-only: bypassed for non-EVM chains (e.g. SVM has no nonce queueing,
	// so in-flight events don't block each other).
	PerChainCap = 16
	// ConsecutiveWaitThreshold: after this many consecutive polls where a chain
	// has in-flight events, use finalized nonce to recover from stuck nonces
	// (~200s at 10s poll).
	// EVM-only: SVM doesn't use a nonce, so stuck-nonce recovery is meaningless.
	ConsecutiveWaitThreshold = 20
	// staleValidatorsHaltMultiplier: if the cached validator set is older than
	// this many pollInterval ticks, it is cleared
	staleValidatorsHaltMultiplier = 10
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
	pushCore        PushCoreClient
	keyshareManager *keyshare.Manager
	chains          *chains.Chains

	// Config
	validatorAddress string
	coordinatorRange uint64
	pollInterval     time.Duration
	logger           zerolog.Logger
	send             SendFunc

	// Lifecycle and cache
	mu                      sync.RWMutex
	running                 bool
	stopCh                  chan struct{}
	allValidators           []*types.UniversalValidator
	lastValidatorsRefreshAt time.Time // zero until first successful refresh

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
	pushCore PushCoreClient,
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

// validatorsSnapshot returns a read-only snapshot of the cached validator set.
// Returns nil if the cache is stale
func (c *Coordinator) validatorsSnapshot() []*types.UniversalValidator {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.lastValidatorsRefreshAt.IsZero() {
		return nil
	}
	age := time.Since(c.lastValidatorsRefreshAt)
	if age > c.pollInterval*time.Duration(staleValidatorsHaltMultiplier) {
		if c.allValidators != nil {
			c.logger.Warn().Dur("age", age).Msg("validator cache exceeded staleness threshold; clearing")
			c.allValidators = nil
		}
		return nil
	}
	return c.allValidators
}

// Validators returns the cached validator set snapshot, or nil if stale.
// Exposed for sessionmanager broadcast fanout.
func (c *Coordinator) Validators() []*types.UniversalValidator {
	return c.validatorsSnapshot()
}

// CancelTracking drops the ackTracking entry for the event if present.
// Used by sessionmanager when a signature_broadcast arrives for an event
// this UV is also coordinating, so no further BEGIN is sent.
func (c *Coordinator) CancelTracking(eventID string) {
	c.ackMu.Lock()
	delete(c.ackTracking, eventID)
	c.ackMu.Unlock()
}

// GetPartyIDFromPeerID gets the partyID (validator address) for a given peerID.
func (c *Coordinator) GetPartyIDFromPeerID(_ context.Context, peerID string) (string, error) {
	for _, v := range c.validatorsSnapshot() {
		if v.NetworkInfo != nil && v.NetworkInfo.PeerId == peerID {
			if v.IdentifyInfo != nil {
				return v.IdentifyInfo.CoreValidatorAddress, nil
			}
		}
	}
	return "", fmt.Errorf("peerID %s not found in validators", peerID)
}

// GetPeerIDFromPartyID gets the peerID for a given partyID (validator address).
func (c *Coordinator) GetPeerIDFromPartyID(_ context.Context, partyID string) (string, error) {
	for _, v := range c.validatorsSnapshot() {
		if v.IdentifyInfo != nil && v.IdentifyInfo.CoreValidatorAddress == partyID {
			if v.NetworkInfo != nil {
				return v.NetworkInfo.PeerId, nil
			}
		}
	}
	return "", fmt.Errorf("partyID %s not found in validators", partyID)
}

// GetMultiAddrsFromPeerID gets the multiaddrs for a given peerID.
func (c *Coordinator) GetMultiAddrsFromPeerID(_ context.Context, peerID string) ([]string, error) {
	for _, v := range c.validatorsSnapshot() {
		if v.NetworkInfo != nil && v.NetworkInfo.PeerId == peerID {
			return v.NetworkInfo.MultiAddrs, nil
		}
	}
	return nil, fmt.Errorf("peerID %s not found in validators", peerID)
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
		return false, fmt.Errorf("failed to get latest block: %w", err)
	}

	allValidators := c.validatorsSnapshot()

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

// DeriveEVMAddressFromPubkey derives an EVM address from a hex-encoded compressed secp256k1 public key.
func DeriveEVMAddressFromPubkey(pubkeyHex string) (string, error) {
	pubkeyHex = strings.TrimPrefix(strings.TrimSpace(pubkeyHex), "0x")
	pubkeyBytes, err := hex.DecodeString(pubkeyHex)
	if err != nil {
		return "", fmt.Errorf("failed to decode public key: %w", err)
	}
	if len(pubkeyBytes) != 33 {
		return "", fmt.Errorf("invalid public key length: %d bytes (expected 33)", len(pubkeyBytes))
	}
	vkX, vkY := secp256k1.DecompressPubkey(pubkeyBytes)
	if vkX == nil || vkY == nil {
		return "", fmt.Errorf("failed to decompress public key")
	}
	xBytes := vkX.FillBytes(make([]byte, 32))
	yBytes := vkY.FillBytes(make([]byte, 32))
	addressBytes := crypto.Keccak256(append(xBytes, yBytes...))[12:]
	return "0x" + hex.EncodeToString(addressBytes), nil
}

// GetTSSAddress returns the TSS ECDSA address derived from the current TSS public key (compressed secp256k1).
func (c *Coordinator) GetTSSAddress(ctx context.Context) (string, error) {
	key, err := c.pushCore.GetCurrentKey(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get current TSS key: %w", err)
	}
	if key == nil || key.TssPubkey == "" {
		return "", fmt.Errorf("no TSS key found")
	}
	return DeriveEVMAddressFromPubkey(key.TssPubkey)
}

// GetEligibleUV returns ALL eligible validators for the given protocol type (no random selection).
// Used by the session manager to check whether a setup-message sender is eligible to participate.
// For SIGN coordinator setup the coordinator calls getSignParticipants (random threshold subset).
func (c *Coordinator) GetEligibleUV(protocolType string) []*types.UniversalValidator {
	allValidators := c.validatorsSnapshot()
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

	c.logger.Debug().Msg("starting coordinator")
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

	c.logger.Debug().Msg("stopping coordinator")
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
	c.lastValidatorsRefreshAt = time.Now()
	c.mu.Unlock()

	c.logger.Debug().Int("count", len(allValidators)).Msg("updated validators cache")
}

// processConfirmedEvents checks if this node is the coordinator for the current block range and,
// if so, fetches CONFIRMED events from the database and drives them through TSS setup.
// Called on every poll tick; returns early (no-op) when this node is not coordinator.
func (c *Coordinator) processConfirmedEvents(ctx context.Context) error {
	currentBlock, err := c.pushCore.GetLatestBlock(ctx)
	if err != nil {
		return fmt.Errorf("failed to get latest block: %w", err)
	}

	allValidators := c.validatorsSnapshot()
	if len(allValidators) == 0 {
		return nil // No validators, skip
	}

	// Check if this node is the coordinator for the current block range.
	// Use coordinatorAddressForBlock directly so we don't make a second GetLatestBlock RPC call.
	if coordinatorAddressForBlock(allValidators, c.coordinatorRange, currentBlock) != c.validatorAddress {
		c.logger.Debug().Msg("processConfirmedEvents: not coordinator, skipping")
		return nil
	}

	c.logger.Debug().Msg("processConfirmedEvents: we ARE coordinator, processing events")

	events, err := c.eventStore.GetNonExpiredConfirmedEvents(currentBlock, 10, 0)
	if err != nil {
		return fmt.Errorf("failed to get confirmed events: %w", err)
	}

	inFlightPerChain, err := c.getInFlightSignCountPerChain()
	if err != nil {
		return fmt.Errorf("failed to get in-flight sign count per chain: %w", err)
	}

	// Only surface at Info when we actually have events to process; otherwise
	// the per-poll Debug above is sufficient and avoids steady-state log noise.
	if len(events) > 0 {
		c.logger.Info().
			Int("count", len(events)).
			Uint64("current_block", currentBlock).
			Msg("found confirmed events")
	}

	// Per-chain nonce cache: fetched once per chain per poll, then incremented locally (n, n+1, n+2, …).
	nonceByChain := make(map[string]uint64)
	skippedChains := make(map[string]bool)

	for _, event := range events {
		var assignedNonce *uint64
		if event.Type == store.EventTypeSignOutbound || event.Type == store.EventTypeSignFundMigrate {
			var chain string
			if event.Type == store.EventTypeSignFundMigrate {
				chain = extractFundMigrateChain(event.EventData)
			} else {
				chain = extractDestinationChain(event.EventData)
			}
			if chain == "" {
				continue
			}

			// Skip if outbound is disabled for destination chain (fund migrations are exempt)
			if event.Type != store.EventTypeSignFundMigrate && !c.chains.IsChainOutboundEnabled(chain) {
				c.logger.Warn().
					Str("chain", chain).
					Str("event_id", event.EventID).
					Msg("outbound disabled for destination chain, skipping TSS signing")
				continue
			}

			// For FUND_MIGRATE, use old TSS address for nonce lookup
			if event.Type == store.EventTypeSignFundMigrate {
				nonce, err := c.assignFundMigrateNonce(ctx, event, chain)
				if err != nil {
					c.logger.Error().Err(err).Str("event_id", event.EventID).Msg("failed to assign fund migration nonce")
					continue
				}
				assignedNonce = &nonce
			} else {
				nonce, ok := c.assignSignNonce(ctx, event, chain, inFlightPerChain, nonceByChain, skippedChains)
				if !ok {
					continue
				}
				assignedNonce = &nonce
			}
		}

		c.logger.Debug().
			Str("event_id", event.EventID).
			Str("type", event.Type).
			Uint64("block_height", event.BlockHeight).
			Msg("processing event as coordinator")
		// For SIGN/FUND_MIGRATE: pick a random threshold subset (>2/3 of eligible) rather than all eligible.
		// A threshold subset suffices for signing and is more resilient when some nodes are offline.
		// For all other protocols (keygen, keyrefresh, quorum_change), all eligible must participate.
		var participants []*types.UniversalValidator
		if event.Type == store.EventTypeSignOutbound || event.Type == store.EventTypeSignFundMigrate {
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
	var unsignedTxReq *common.UnsignedSigningReq
	var err error
	switch event.Type {
	case store.EventTypeKeygen, store.EventTypeKeyrefresh:
		// Keygen and keyrefresh use the same setup structure
		setupData, err = c.createKeygenSetup(threshold, partyIDs)
	case store.EventTypeQuorumChange:
		setupData, err = c.createQcSetup(ctx, threshold, partyIDs, sortedParticipants)
	case store.EventTypeSignOutbound:
		setupData, unsignedTxReq, err = c.createSignSetup(ctx, event.EventData, partyIDs, assignedNonce)
	case store.EventTypeSignFundMigrate:
		setupData, unsignedTxReq, err = c.createFundMigrationSignSetup(ctx, event.EventData, partyIDs, assignedNonce)
	default:
		err = fmt.Errorf("unknown protocol type: %s", event.Type)
	}

	if err != nil {
		return fmt.Errorf("failed to create setup message for event %s: %w", event.EventID, err)
	}

	// Create and send setup message to all participants
	setupMsg := Message{
		Type:               MessageTypeSetup,
		EventID:            event.EventID,
		Payload:            setupData,
		Participants:       partyIDs,
		UnsignedSigningReq: unsignedTxReq, // nil for non-sign events
	}
	setupMsgBytes, err := json.Marshal(setupMsg)
	if err != nil {
		return fmt.Errorf("failed to marshal setup message for event %s: %w", event.EventID, err)
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
			c.logger.Debug().
				Str("event_id", event.EventID).
				Str("receiver", receiverAddr).
				Msg("sent setup message to participant")
		}
	}

	return nil
}

// createFundMigrationSignSetup creates a sign setup message for fund migration.
// Uses the OLD key (not the current key) to sign a transaction moving funds from old TSS to current TSS.
func (c *Coordinator) createFundMigrationSignSetup(ctx context.Context, eventData []byte, partyIDs []string, assignedNonce *uint64) ([]byte, *common.UnsignedSigningReq, error) {
	var migrationData utsstypes.FundMigrationInitiatedEventData
	if err := json.Unmarshal(eventData, &migrationData); err != nil {
		return nil, nil, fmt.Errorf("failed to unmarshal fund migration event data: %w", err)
	}

	// Load old keyshare as a sanity check; keyID bytes are derived from the string.
	if _, err := c.keyshareManager.Get(migrationData.OldKeyID); err != nil {
		return nil, nil, fmt.Errorf("failed to load keyshare for old keyId %s: %w", migrationData.OldKeyID, err)
	}
	keyIDBytes := deriveKeyIDBytes(migrationData.OldKeyID)

	signingReq, err := c.buildFundMigrationTransaction(ctx, eventData, assignedNonce, nil /* query chain for balance */)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to build fund migration transaction: %w", err)
	}

	participantIDs := make([]byte, 0, len(partyIDs)*10)
	for i, partyID := range partyIDs {
		if i > 0 {
			participantIDs = append(participantIDs, 0)
		}
		participantIDs = append(participantIDs, []byte(partyID)...)
	}

	setupData, err := session.DklsSignSetupMsgNew(keyIDBytes, nil, signingReq.SigningHash, participantIDs)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create fund migration sign setup: %w", err)
	}

	return setupData, signingReq, nil
}

// buildFundMigrationTransaction parses event data and returns the signing
// request for sweeping old-TSS funds to the current TSS. If claimedAmount is
// non-nil, the balance is reconstructed as amount + gas + L1 instead of
// queried from chain — used by the ACK verify path to rebuild the hash
// deterministically without racing a successful sweep.
func (c *Coordinator) buildFundMigrationTransaction(ctx context.Context, eventData []byte, assignedNonce *uint64, claimedAmount *big.Int) (*common.UnsignedSigningReq, error) {
	if assignedNonce == nil {
		return nil, fmt.Errorf("assigned nonce is required for fund migration transaction")
	}
	var migrationData utsstypes.FundMigrationInitiatedEventData
	if err := json.Unmarshal(eventData, &migrationData); err != nil {
		return nil, fmt.Errorf("unmarshal fund migration event data: %w", err)
	}
	oldTSSAddr, err := DeriveEVMAddressFromPubkey(migrationData.OldTssPubkey)
	if err != nil {
		return nil, fmt.Errorf("derive old TSS address: %w", err)
	}
	currentTSSAddr, err := DeriveEVMAddressFromPubkey(migrationData.CurrentTssPubkey)
	if err != nil {
		return nil, fmt.Errorf("derive current TSS address: %w", err)
	}
	if c.chains == nil {
		return nil, fmt.Errorf("chains manager not configured")
	}
	client, err := c.chains.GetClient(migrationData.Chain)
	if err != nil {
		return nil, fmt.Errorf("get client for chain %s: %w", migrationData.Chain, err)
	}
	builder, err := client.GetTxBuilder()
	if err != nil {
		return nil, fmt.Errorf("get tx builder for chain %s: %w", migrationData.Chain, err)
	}

	gasPrice := new(big.Int)
	gasPrice.SetString(migrationData.GasPrice, 10)
	l1GasFee := new(big.Int)
	l1GasFee.SetString(migrationData.L1GasFee, 10)

	var balance *big.Int
	if claimedAmount != nil {
		// balance = amount + gas + L1; inverse of computeFundMigrationTransfer
		balance = new(big.Int).Set(claimedAmount)
		balance.Add(balance, new(big.Int).Mul(gasPrice, new(big.Int).SetUint64(migrationData.GasLimit)))
		if l1GasFee.Sign() > 0 {
			balance.Add(balance, l1GasFee)
		}
	}

	return builder.GetFundMigrationSigningRequest(ctx, &common.FundMigrationData{
		From:     oldTSSAddr,
		To:       currentTSSAddr,
		GasPrice: gasPrice,
		GasLimit: migrationData.GasLimit,
		L1GasFee: l1GasFee,
		Balance:  balance,
	}, *assignedNonce)
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
		return nil, fmt.Errorf("failed to create setup: %w", err)
	}
	return setupData, nil
}

// createSignSetup creates a sign setup message and returns the unsigned transaction request.
// Uses the TxBuilder to build the actual transaction for the destination chain.
// assignedNonce must be set for SIGN (coordinator-assigned nonce); participants use it for verification.
// Returns the setup data, unsigned transaction request (for participant verification), and error.
func (c *Coordinator) createSignSetup(ctx context.Context, eventData []byte, partyIDs []string, assignedNonce *uint64) ([]byte, *common.UnsignedSigningReq, error) {
	// Get current TSS keyId from pushCore
	key, err := c.pushCore.GetCurrentKey(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get current TSS keyId: %w", err)
	}
	if key == nil {
		return nil, nil, fmt.Errorf("no TSS key exists")
	}
	keyIDStr := key.KeyId

	// Load keyshare to ensure it exists (validation)
	keyshareBytes, err := c.keyshareManager.Get(keyIDStr)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load keyshare for keyId %s: %w", keyIDStr, err)
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
		return nil, nil, fmt.Errorf("failed to build sign transaction: %w", err)
	}

	setupData, err := session.DklsSignSetupMsgNew(keyIDBytes, nil, signingReq.SigningHash, participantIDs)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create sign setup: %w", err)
	}

	return setupData, signingReq, nil
}

// buildSignTransaction builds the outbound transaction using the appropriate TxBuilder.
func (c *Coordinator) buildSignTransaction(ctx context.Context, eventData []byte, assignedNonce *uint64) (*common.UnsignedSigningReq, error) {
	if len(eventData) == 0 {
		return nil, fmt.Errorf("event data is empty")
	}

	var data uexecutortypes.OutboundCreatedEvent
	if err := json.Unmarshal(eventData, &data); err != nil {
		return nil, fmt.Errorf("failed to unmarshal outbound event data: %w", err)
	}

	if data.TxID == "" {
		return nil, fmt.Errorf("outbound event missing tx_id")
	}

	if data.DestinationChain == "" {
		return nil, fmt.Errorf("outbound event missing destination_chain")
	}

	if c.chains == nil {
		return nil, fmt.Errorf("chains manager not configured")
	}

	// Get the client for the destination chain
	client, err := c.chains.GetClient(data.DestinationChain)
	if err != nil {
		return nil, fmt.Errorf("failed to get client for chain %s: %w", data.DestinationChain, err)
	}

	// Get the builder from the client
	builder, err := client.GetTxBuilder()
	if err != nil {
		return nil, fmt.Errorf("failed to get tx builder for chain %s: %w", data.DestinationChain, err)
	}

	// Get the signing request (nonce is required for SIGN)
	if assignedNonce == nil {
		return nil, fmt.Errorf("assigned nonce is required for sign transaction")
	}
	signingReq, err := builder.GetOutboundSigningRequest(ctx, &data, *assignedNonce)
	if err != nil {
		return nil, fmt.Errorf("failed to get outbound signing request: %w", err)
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
		return nil, fmt.Errorf("failed to get current TSS keyId: %w", err)
	}
	if key == nil {
		return nil, fmt.Errorf("no TSS key exists")
	}
	keyIDStr := key.KeyId

	// Load old keyshare to get the key we're changing
	oldKeyshareBytes, err := c.keyshareManager.Get(keyIDStr)
	if err != nil {
		return nil, fmt.Errorf("failed to load keyshare for keyId %s: %w", keyIDStr, err)
	}

	// Load keyshare handle from bytes
	oldKeyshareHandle, err := session.DklsKeyshareFromBytes(oldKeyshareBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to load keyshare handle: %w", err)
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
		return nil, fmt.Errorf("failed to create quorumchange setup: %w", err)
	}
	return setupData, nil
}

// getEligibleForProtocol returns ALL eligible validators for a protocol type (no random selection).
// Used by the session manager for eligibility validation and by the coordinator for keygen,
// keyrefresh, and quorum_change setup (all eligible validators must participate in those protocols).
// For SIGN coordinator setup, use getSignParticipants instead (random threshold subset).
func getEligibleForProtocol(protocolType string, allValidators []*types.UniversalValidator) []*types.UniversalValidator {
	switch protocolType {
	case store.EventTypeKeygen, store.EventTypeQuorumChange:
		// Active + Pending Join
		return getQuorumChangeParticipants(allValidators)
	case store.EventTypeKeyrefresh:
		// Active + Pending Leave
		return getSignEligible(allValidators)
	case store.EventTypeSignOutbound, store.EventTypeSignFundMigrate:
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

	// Non-EVM chains (SVM today) have no nonce semantics — every tx carries
	// its own blockhash and replay protection (ExecutedSubTx PDA on SVM).
	// In-flight events don't block each other, so PerChainCap and the
	// wait-then-recover dance are EVM-only optimizations. For non-EVM chains
	// skip straight to nonce fetch (which returns 0 for SVM).
	isEVM := c.chains != nil && c.chains.IsEVMChain(chain)

	// ── Subsequent event for this chain (nonce already fetched this poll) ──
	if _, exists := nonceByChain[chain]; exists {
		if isEVM && inFlightPerChain[chain] >= PerChainCap {
			return 0, false
		}
		nonceByChain[chain]++
		inFlightPerChain[chain]++
		return nonceByChain[chain], true
	}

	// ── First event for this chain this poll ──
	// Decide: process normally, wait (skip), or recover with finalized nonce.
	useFinalized := false

	if isEVM && inFlightPerChain[chain] > 0 {
		c.chainWaitMu.Lock()
		consecutiveWait := c.consecutiveWaitPerChain[chain]
		if consecutiveWait < ConsecutiveWaitThreshold {
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
		c.chainWaitMu.Unlock()

		// Patience exhausted — recover with finalized nonce.
		// Cap is intentionally bypassed: stuck events have stale nonces and will
		// be cleared by broadcaster → resolver → REVERTED.
		useFinalized = true
		c.logger.Debug().
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
		return 0, fmt.Errorf("chains manager not configured")
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

// extractFundMigrateChain extracts the chain field from FundMigrationInitiatedEventData JSON.
func extractFundMigrateChain(eventData []byte) string {
	if len(eventData) == 0 {
		return ""
	}
	var data struct {
		Chain string `json:"chain"`
	}
	if err := json.Unmarshal(eventData, &data); err != nil {
		return ""
	}
	return data.Chain
}

// assignFundMigrateNonce resolves the nonce for a FUND_MIGRATE event using the old TSS address.
// Since the chain ensures at most 1 migration per chain and no pending outbounds,
// we simply fetch the finalized nonce for the old TSS address.
func (c *Coordinator) assignFundMigrateNonce(ctx context.Context, event store.Event, chain string) (uint64, error) {
	var migrationData utsstypes.FundMigrationInitiatedEventData
	if err := json.Unmarshal(event.EventData, &migrationData); err != nil {
		return 0, fmt.Errorf("failed to unmarshal fund migration event data: %w", err)
	}

	oldTSSAddr, err := DeriveEVMAddressFromPubkey(migrationData.OldTssPubkey)
	if err != nil {
		return 0, fmt.Errorf("failed to derive old TSS address: %w", err)
	}

	if c.chains == nil {
		return 0, fmt.Errorf("chains manager not configured")
	}
	client, err := c.chains.GetClient(chain)
	if err != nil {
		return 0, err
	}
	builder, err := client.GetTxBuilder()
	if err != nil {
		return 0, err
	}

	return builder.GetNextNonce(ctx, oldTSSAddr, true)
}
