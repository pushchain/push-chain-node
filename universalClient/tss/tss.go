package tss

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"time"

	"github.com/pushchain/push-chain-node/universalClient/db"
	"github.com/pushchain/push-chain-node/universalClient/store"
	"github.com/pushchain/push-chain-node/universalClient/tss/keyshare"
	"github.com/pushchain/push-chain-node/universalClient/tss/uv"
	"github.com/rs/zerolog"
)

const (
	defaultCoordinatorRangeSize = 100 // Blocks per coordinator epoch
	defaultSigningThreshold     = 66  // Percentage threshold for signing
)

// TSS orchestrates TSS protocol execution, network management, and event processing
type TSS struct {
	ctx    context.Context
	logger zerolog.Logger

	// Database
	dbManager   *db.ChainDBManager
	pushChainDB *db.DB // Database for Push Chain (where TSS events are stored)

	// Network (TODO: implement libp2p network initialization)
	// network *network.Network

	// UV management
	uvManager *uv.Manager

	// Keyshare management
	keyshareManager *keyshare.Manager

	// Configuration
	myValidatorAddress   string // This node's validator address
	pollInterval         time.Duration
	coordinatorRangeSize int64
	signingThreshold     int // Percentage threshold for signing (default 66)

	// Protocol handlers (TODO: implement DKLS protocols)
	// keyGenProtocol     *dkls.KeyGen
	// keyRefreshProtocol *dkls.KeyRefresh
	// signProtocol       *dkls.Sign
}

// Config holds configuration for the TSS
type Config struct {
	MyValidatorAddress   string
	HomeDir              string
	EncryptionPassword   string
	PollInterval         time.Duration
	CoordinatorRangeSize int64
	SigningThreshold     int // Percentage threshold for signing (default 66)
}

// New creates a new TSS instance
func New(
	ctx context.Context,
	logger zerolog.Logger,
	dbManager *db.ChainDBManager,
	pushChainID string,
	cfg Config,
) (*TSS, error) {
	// Get Push Chain database
	pushChainDB, err := dbManager.GetChainDB(pushChainID)
	if err != nil {
		return nil, fmt.Errorf("failed to get Push Chain database: %w", err)
	}

	// Initialize keyshare manager
	keyshareMgr, err := keyshare.NewManager(cfg.HomeDir, cfg.EncryptionPassword)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize keyshare manager: %w", err)
	}

	// Set defaults
	pollInterval := cfg.PollInterval
	if pollInterval == 0 {
		pollInterval = 5 * time.Second
	}

	rangeSize := cfg.CoordinatorRangeSize
	if rangeSize == 0 {
		rangeSize = defaultCoordinatorRangeSize
	}

	signingThreshold := cfg.SigningThreshold
	if signingThreshold == 0 {
		signingThreshold = defaultSigningThreshold
	}

	// Initialize UV manager
	uvMgr := uv.New(ctx, logger, cfg.MyValidatorAddress, rangeSize)

	return &TSS{
		ctx:                  ctx,
		logger:               logger.With().Str("component", "tss").Logger(),
		dbManager:            dbManager,
		pushChainDB:          pushChainDB,
		uvManager:            uvMgr,
		keyshareManager:      keyshareMgr,
		myValidatorAddress:   cfg.MyValidatorAddress,
		pollInterval:         pollInterval,
		coordinatorRangeSize: rangeSize,
		signingThreshold:     signingThreshold,
	}, nil
}

// Start begins the TSS main event processing loop
func (t *TSS) Start() error {
	t.logger.Info().Msg("starting TSS")

	// Start UV manager (fetches and refreshes validators)
	if err := t.uvManager.Start(); err != nil {
		return fmt.Errorf("failed to start UV manager: %w", err)
	}

	// TODO: Initialize libp2p network
	// if err := m.initializeNetwork(); err != nil {
	// 	return fmt.Errorf("failed to initialize network: %w", err)
	// }

	// Start event processing loop
	go t.eventProcessingLoop()

	t.logger.Info().Msg("TSS service started")
	return nil
}

// Stop stops the TSS
func (t *TSS) Stop() error {
	t.logger.Info().Msg("stopping TSS service")

	// TODO: Close network connections
	// if err := m.network.Close(); err != nil {
	// 	return fmt.Errorf("failed to close network: %w", err)
	// }

	t.logger.Info().Msg("TSS stopped")
	return nil
}

// eventProcessingLoop continuously polls the database for PENDING TSS events
func (t *TSS) eventProcessingLoop() {
	ticker := time.NewTicker(t.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-t.ctx.Done():
			t.logger.Info().Msg("event processing loop stopped")
			return
		case <-ticker.C:
			if err := t.processPendingEvents(); err != nil {
				t.logger.Error().Err(err).Msg("failed to process pending events")
			}
		}
	}
}

// processPendingEvents queries the database for PENDING events and processes them
func (t *TSS) processPendingEvents() error {
	// TODO: Implement DB query for pending events
	pendingEvents := make([]store.TSSEvent, 0)

	// Process each pending event
	for _, event := range pendingEvents {
		if err := t.processEvent(&event); err != nil {
			t.logger.Error().
				Err(err).
				Str("event_id", event.EventID).
				Str("process_type", event.ProtocolType).
				Msg("failed to process event")
			// Continue processing other events even if one fails
		}
	}

	return nil
}

// processEvent processes a single TSS event
func (t *TSS) processEvent(event *store.TSSEvent) error {
	t.logger.Info().
		Str("event_id", event.EventID).
		Str("process_type", event.ProtocolType).
		Uint64("block_number", event.BlockNumber).
		Msg("processing TSS event")

	// Determine process type
	processType := TssProcessType(event.ProtocolType)

	// Check if this node is the coordinator (based on latest block)
	isCoord, err := t.uvManager.IsCoordinator()
	if err != nil {
		return fmt.Errorf("failed to check coordinator status: %w", err)
	}

	if !isCoord {
		t.logger.Debug().
			Str("event_id", event.EventID).
			Msg("not the coordinator for this event, skipping")
		return nil
	}

	// Get validators for party selection
	validators := t.uvManager.GetUVs()

	// Update event status to IN_PROGRESS
	// TODO: Implement DB update
	// err = t.pushChainDB.client.Model(event).Update("status", "IN_PROGRESS").Error
	// if err != nil {
	// 	return fmt.Errorf("failed to update event status: %w", err)
	// }

	// Process based on process type
	switch processType {
	case TssProcessTypeKeyGen:
		return t.handleKeyGen(event, validators)
	case TssProcessTypeKeyRefresh:
		return t.handleKeyRefresh(event, validators)
	case TssProcessTypeSign:
		return t.handleSign(event, validators)
	default:
		return fmt.Errorf("unknown process type: %s", event.ProtocolType)
	}
}

// handleKeyGen processes a KeyGen event
func (t *TSS) handleKeyGen(event *store.TSSEvent, validators []*uv.UniversalValidator) error {
	t.logger.Info().
		Str("event_id", event.EventID).
		Msg("handling KeyGen event")

	// Select parties (100% of eligible validators for KeyGen)
	partyAddresses, err := t.partySelection(validators, TssProcessTypeKeyGen)
	if err != nil {
		return fmt.Errorf("failed to select parties: %w", err)
	}

	t.logger.Info().
		Str("event_id", event.EventID).
		Int("party_count", len(partyAddresses)).
		Msg("selected parties for KeyGen")

	// TODO: Convert party addresses to PartyInfo with libp2p peer IDs
	// parties := m.convertToPartyInfo(partyAddresses)

	// TODO: Initialize and execute DKLS KeyGen protocol
	// config := &dkls.ProtocolConfig{
	// 	ProtocolType: dkls.ProtocolTypeKeyGen,
	// 	Threshold:    calculateThreshold(len(parties)),
	// 	Parties:      parties,
	// 	KeyID:        []byte(event.KeyID),
	// }
	//
	// keyGen := dkls.NewKeyGen(config)
	// if err := keyGen.Start(); err != nil {
	// 	return fmt.Errorf("failed to start KeyGen protocol: %w", err)
	// }
	//
	// // Process protocol messages
	// for !keyGen.IsComplete() {
	// 	// Get outgoing messages and send via network
	// 	outgoing, err := keyGen.GetOutgoingMessages()
	// 	if err != nil {
	// 		return fmt.Errorf("failed to get outgoing messages: %w", err)
	// 	}
	//
	// 	// TODO: Send messages via libp2p network
	// 	// m.network.SendMessages(outgoing)
	//
	// 	// TODO: Receive and process incoming messages
	// 	// incoming := m.network.ReceiveMessages()
	// 	// for from, msg := range incoming {
	// 	// 	if err := keyGen.ProcessMessage(from, msg); err != nil {
	// 	// 		return fmt.Errorf("failed to process message: %w", err)
	// 	// 	}
	// 	// }
	// }
	//
	// // Get keyshare result
	// keyshareBytes, err := keyGen.GetResult()
	// if err != nil {
	// 	return fmt.Errorf("failed to get KeyGen result: %w", err)
	// }
	//
	// // Store keyshare
	// if err := m.keyshareManager.Store(keyshareBytes, event.KeyID); err != nil {
	// 	return fmt.Errorf("failed to store keyshare: %w", err)
	// }

	// TODO: Update event status to SUCCESS
	// err = t.pushChainDB.client.Model(event).Update("status", "SUCCESS").Error
	// if err != nil {
	// 	return fmt.Errorf("failed to update event status: %w", err)
	// }

	t.logger.Info().
		Str("event_id", event.EventID).
		Msg("KeyGen event completed successfully")

	return nil
}

// handleKeyRefresh processes a KeyRefresh event
func (t *TSS) handleKeyRefresh(event *store.TSSEvent, validators []*uv.UniversalValidator) error {
	t.logger.Info().
		Str("event_id", event.EventID).
		Msg("handling KeyRefresh event")

	// Select parties (100% of eligible validators for KeyRefresh)
	partyAddresses, err := t.partySelection(validators, TssProcessTypeKeyRefresh)
	if err != nil {
		return fmt.Errorf("failed to select parties: %w", err)
	}

	t.logger.Info().
		Str("event_id", event.EventID).
		Int("party_count", len(partyAddresses)).
		Msg("selected parties for KeyRefresh")

	// TODO: Implement KeyRefresh protocol execution (similar to KeyGen)
	// Similar structure to handleKeyGen but with existing keyshare loaded

	// TODO: Update event status to SUCCESS
	t.logger.Info().
		Str("event_id", event.EventID).
		Msg("KeyRefresh event completed successfully")

	return nil
}

// handleSign processes a Sign event
func (t *TSS) handleSign(event *store.TSSEvent, validators []*uv.UniversalValidator) error {
	t.logger.Info().
		Str("event_id", event.EventID).
		Msg("handling Sign event")

	// Select parties (>66% randomly selected for Sign)
	partyAddresses, err := t.partySelection(validators, TssProcessTypeSign)
	if err != nil {
		return fmt.Errorf("failed to select parties: %w", err)
	}

	t.logger.Info().
		Str("event_id", event.EventID).
		Int("party_count", len(partyAddresses)).
		Msg("selected parties for Sign")

	// TODO: Load keyshare for the key ID
	// keyshareBytes, err := t.keyshareManager.Get(event.KeyID)
	// if err != nil {
	// 	return fmt.Errorf("failed to load keyshare: %w", err)
	// }

	// TODO: Initialize and execute DKLS Sign protocol
	// config := &dkls.ProtocolConfig{
	// 	ProtocolType: dkls.ProtocolTypeSign,
	// 	Threshold:    calculateThreshold(len(parties)),
	// 	Parties:      parties,
	// 	KeyID:        []byte(event.KeyID),
	// 	MessageHash:  event.MessageHash,
	// }
	//
	// sign := dkls.NewSign(config)
	// if err := sign.Start(); err != nil {
	// 	return fmt.Errorf("failed to start Sign protocol: %w", err)
	// }
	//
	// // Process protocol messages (similar to KeyGen)
	// // ...
	//
	// // Get signature result
	// signature, err := sign.GetResult()
	// if err != nil {
	// 	return fmt.Errorf("failed to get Sign result: %w", err)
	// }

	// TODO: Update TSS event status to SUCCESS
	// err = t.pushChainDB.client.Model(event).Update("status", "SUCCESS").Error
	// if err != nil {
	// 	return fmt.Errorf("failed to update event status: %w", err)
	// }

	// TODO: Create entry in external chain DB with signature and status=PENDING
	// externalSig := &store.ExternalChainSignature{
	// 	TSSEventID:  event.ID,
	// 	ChainID:      targetChainID, // Extract from event data
	// 	Status:       "PENDING",
	// 	Signature:    signature,
	// 	MessageHash:  event.MessageHash,
	// }
	// if err := t.pushChainDB.client.Create(externalSig).Error; err != nil {
	// 	return fmt.Errorf("failed to create external chain signature entry: %w", err)
	// }

	t.logger.Info().
		Str("event_id", event.EventID).
		Msg("Sign event completed successfully")

	return nil
}

// partySelection selects parties for a TSS process based on the process type
// Returns the selected validator addresses
func (t *TSS) partySelection(
	allValidators []*uv.UniversalValidator,
	processType TssProcessType,
) ([]string, error) {
	// Filter eligible validators based on process type
	eligibleValidators := t.filterEligibleValidators(allValidators, processType)
	if len(eligibleValidators) == 0 {
		return nil, uv.ErrNoEligibleValidators
	}

	switch processType {
	case TssProcessTypeKeyGen, TssProcessTypeKeyRefresh:
		// KeyGen/KeyRefresh: Select 100% of eligible validators
		return t.selectAllParties(eligibleValidators), nil

	case TssProcessTypeSign:
		// Sign: Select >66% randomly from eligible validators
		return t.selectRandomThreshold(eligibleValidators, t.signingThreshold)

	default:
		return nil, fmt.Errorf("invalid process type: %s", processType)
	}
}

// filterEligibleValidators filters validators based on TSS process type eligibility
func (t *TSS) filterEligibleValidators(
	allValidators []*uv.UniversalValidator,
	processType TssProcessType,
) []*uv.UniversalValidator {
	eligible := make([]*uv.UniversalValidator, 0, len(allValidators))
	for _, v := range allValidators {
		if t.isEligibleForProcess(v, processType) {
			eligible = append(eligible, v)
		}
	}
	return eligible
}

// isEligibleForProcess checks if a validator is eligible for a given TSS process type
func (t *TSS) isEligibleForProcess(v *uv.UniversalValidator, processType TssProcessType) bool {
	switch processType {
	case TssProcessTypeKeyGen, TssProcessTypeKeyRefresh:
		// KeyGen/KeyRefresh: ACTIVE + PENDING_JOIN
		return v.Status == uv.UVStatusActive || v.Status == uv.UVStatusPendingJoin
	case TssProcessTypeSign:
		// Sign: ACTIVE + PENDING_LEAVE
		return v.Status == uv.UVStatusActive || v.Status == uv.UVStatusPendingLeave
	default:
		return false
	}
}

// selectAllParties returns all validator addresses
func (t *TSS) selectAllParties(validators []*uv.UniversalValidator) []string {
	addresses := make([]string, len(validators))
	for i, v := range validators {
		addresses[i] = v.ValidatorAddress
	}
	return addresses
}

// selectRandomThreshold selects a random subset of validators that meets the threshold percentage
func (t *TSS) selectRandomThreshold(
	validators []*uv.UniversalValidator,
	thresholdPercent int,
) ([]string, error) {
	if len(validators) == 0 {
		return nil, uv.ErrNoEligibleValidators
	}

	// Calculate minimum number of validators needed
	// Use ceiling division: (n * threshold + 99) / 100 ensures we round up
	minRequired := (len(validators)*thresholdPercent + 99) / 100
	if minRequired == 0 {
		minRequired = 1 // At least 1 validator
	}

	// Ensure we don't exceed the total number of validators
	if minRequired > len(validators) {
		minRequired = len(validators)
	}

	// If we need all validators, return all
	if minRequired == len(validators) {
		return t.selectAllParties(validators), nil
	}

	// Randomly select validators
	selected := make([]*uv.UniversalValidator, 0, minRequired)
	available := make([]*uv.UniversalValidator, len(validators))
	copy(available, validators)

	for len(selected) < minRequired && len(available) > 0 {
		// Select random index
		idx, err := rand.Int(rand.Reader, big.NewInt(int64(len(available))))
		if err != nil {
			return nil, fmt.Errorf("failed to generate random index: %w", err)
		}

		selectedIdx := int(idx.Int64())
		selected = append(selected, available[selectedIdx])

		// Remove selected validator from available list
		available = append(available[:selectedIdx], available[selectedIdx+1:]...)
	}

	// Convert to addresses
	addresses := make([]string, len(selected))
	for i, v := range selected {
		addresses[i] = v.ValidatorAddress
	}

	return addresses, nil
}
