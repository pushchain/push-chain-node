// Package coordinator provides database-driven TSS event coordination for production deployments.
// It polls for TSS events, determines coordinator role, and triggers TSS operations.
package coordinator

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/pkg/errors"
	"github.com/rs/zerolog"
	"gorm.io/gorm"

	"github.com/pushchain/push-chain-node/universalClient/store"
	"github.com/pushchain/push-chain-node/universalClient/tss"
	"github.com/pushchain/push-chain-node/universalClient/tss/core"
)

const (
	// Event statuses
	StatusPending    = "PENDING"
	StatusInProgress = "IN_PROGRESS"
	StatusSuccess    = "SUCCESS"
	StatusFailed     = "FAILED"
	StatusExpired    = "EXPIRED"

	// Default poll interval
	DefaultPollInterval = 2 * time.Second
	// Default event processing timeout
	DefaultProcessingTimeout = 5 * time.Minute
)

// PushChainDataProvider provides access to Push Chain data including validators and block information.
type PushChainDataProvider interface {
	// GetLatestBlockNum returns the latest block number from the chain.
	GetLatestBlockNum(ctx context.Context) (uint64, error)

	// GetUniversalValidators returns all universal validators.
	GetUniversalValidators(ctx context.Context) ([]*tss.UniversalValidator, error)

	// GetUniversalValidator returns a specific universal validator by its validator address.
	GetUniversalValidator(ctx context.Context, validatorAddress string) (*tss.UniversalValidator, error)
}

// Coordinator orchestrates TSS operations by polling the database for events.
type Coordinator struct {
	db           *gorm.DB
	service      *core.Service
	dataProvider PushChainDataProvider
	partyID      string // This node's party ID (validator address)
	logger       zerolog.Logger

	// Configuration
	pollInterval      time.Duration
	processingTimeout time.Duration
	coordinatorRange  uint64

	// State
	mu           sync.RWMutex
	running      bool
	stopCh       chan struct{}
	processingWg sync.WaitGroup
	activeEvents map[string]context.CancelFunc // eventID -> cancel function
}

// Config holds coordinator configuration.
type Config struct {
	DB           *gorm.DB
	Service      *core.Service
	DataProvider PushChainDataProvider
	PartyID      string
	Logger       zerolog.Logger

	// Optional configuration
	PollInterval      time.Duration
	ProcessingTimeout time.Duration
	CoordinatorRange  uint64
}

// NewCoordinator creates a new coordinator instance.
func NewCoordinator(cfg Config) (*Coordinator, error) {
	if cfg.DB == nil {
		return nil, errors.New("database is required")
	}
	// Service can be nil initially and set later via SetService
	if cfg.DataProvider == nil {
		return nil, errors.New("data provider is required")
	}
	if cfg.PartyID == "" {
		return nil, errors.New("party ID is required")
	}

	if cfg.PollInterval == 0 {
		cfg.PollInterval = DefaultPollInterval
	}
	if cfg.ProcessingTimeout == 0 {
		cfg.ProcessingTimeout = DefaultProcessingTimeout
	}
	if cfg.CoordinatorRange == 0 {
		cfg.CoordinatorRange = 100 // Default range size
	}

	logger := cfg.Logger.With().Str("component", "tss_coordinator").Logger()

	return &Coordinator{
		db:                cfg.DB,
		service:           cfg.Service,
		dataProvider:      cfg.DataProvider,
		partyID:           cfg.PartyID,
		logger:            logger,
		pollInterval:      cfg.PollInterval,
		processingTimeout: cfg.ProcessingTimeout,
		coordinatorRange:  cfg.CoordinatorRange,
		stopCh:            make(chan struct{}),
		activeEvents:      make(map[string]context.CancelFunc),
	}, nil
}

// Start begins polling for TSS events.
func (c *Coordinator) Start(ctx context.Context) error {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return errors.New("coordinator is already running")
	}
	c.running = true
	c.mu.Unlock()

	c.logger.Info().Msg("starting TSS coordinator")

	go c.pollLoop(ctx)
	return nil
}

// Stop stops the coordinator and waits for active operations to complete.
func (c *Coordinator) Stop() error {
	c.mu.Lock()
	if !c.running {
		c.mu.Unlock()
		return nil
	}
	c.running = false
	close(c.stopCh)
	c.mu.Unlock()

	c.logger.Info().Msg("stopping TSS coordinator, waiting for active operations...")

	// Cancel all active event processing
	c.mu.Lock()
	for eventID, cancel := range c.activeEvents {
		c.logger.Debug().Str("event_id", eventID).Msg("canceling active event")
		cancel()
	}
	c.mu.Unlock()

	// Wait for all processing goroutines to finish
	c.processingWg.Wait()

	c.logger.Info().Msg("TSS coordinator stopped")
	return nil
}

func (c *Coordinator) pollLoop(ctx context.Context) {
	ticker := time.NewTicker(c.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.stopCh:
			return
		case <-ticker.C:
			if err := c.processPendingEvents(ctx); err != nil {
				c.logger.Error().Err(err).Msg("error processing pending events")
			}
		}
	}
}

func (c *Coordinator) processPendingEvents(ctx context.Context) error {
	var events []store.TSSEvent
	if err := c.db.Where("status = ?", StatusPending).
		Order("block_number ASC, created_at ASC").
		Find(&events).Error; err != nil {
		return errors.Wrap(err, "failed to query pending events")
	}

	currentBlock, err := c.dataProvider.GetLatestBlockNum(ctx)
	if err != nil {
		return errors.Wrap(err, "failed to get latest block number")
	}

	for _, event := range events {
		// Only process events that are at least 10 blocks before current block
		// This ensures all nodes are synced up properly
		if currentBlock < event.BlockNumber+10 {
			c.logger.Debug().
				Str("event_id", event.EventID).
				Uint64("event_block", event.BlockNumber).
				Uint64("current_block", currentBlock).
				Uint64("blocks_behind", currentBlock-event.BlockNumber).
				Msg("skipping event - waiting for 10 block confirmation")
			continue
		}

		// Check if event has expired
		if event.ExpiryHeight > 0 && currentBlock > event.ExpiryHeight {
			c.logger.Warn().
				Str("event_id", event.EventID).
				Uint64("expiry_height", event.ExpiryHeight).
				Uint64("current_block", currentBlock).
				Msg("event expired, marking as expired")
			if err := c.updateEventStatus(event.EventID, StatusExpired, ""); err != nil {
				c.logger.Error().Err(err).Str("event_id", event.EventID).Msg("failed to mark event as expired")
			}
			continue
		}

		// Check if this event is already being processed
		c.mu.RLock()
		_, alreadyProcessing := c.activeEvents[event.EventID]
		c.mu.RUnlock()
		if alreadyProcessing {
			continue
		}

		// Start processing this event
		c.processingWg.Add(1)
		go func(evt store.TSSEvent) {
			defer c.processingWg.Done()
			c.processEvent(ctx, evt)
		}(event)
	}

	return nil
}

func (c *Coordinator) processEvent(ctx context.Context, event store.TSSEvent) {
	eventCtx, cancel := context.WithTimeout(ctx, c.processingTimeout)
	defer cancel()

	// Register this event as active
	c.mu.Lock()
	c.activeEvents[event.EventID] = cancel
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		delete(c.activeEvents, event.EventID)
		c.mu.Unlock()
	}()

	c.logger.Info().
		Str("event_id", event.EventID).
		Str("protocol", event.ProtocolType).
		Uint64("block_number", event.BlockNumber).
		Msg("processing TSS event")

	// Get all universal validators and filter for active ones
	allValidators, err := c.dataProvider.GetUniversalValidators(eventCtx)
	if err != nil {
		c.logger.Error().
			Err(err).
			Str("event_id", event.EventID).
			Msg("failed to get universal validators")
		c.updateEventStatus(event.EventID, StatusFailed, err.Error())
		return
	}

	// Filter for active validators only
	var participants []*tss.UniversalValidator
	for _, v := range allValidators {
		if v.Status == tss.UVStatusActive {
			participants = append(participants, v)
		}
	}

	if len(participants) == 0 {
		c.logger.Warn().
			Str("event_id", event.EventID).
			Msg("no active participants available")
		c.updateEventStatus(event.EventID, StatusFailed, "no active participants available")
		return
	}

	// Check if this node is in the participant list
	var isParticipant bool
	for _, p := range participants {
		if p.PartyID() == c.partyID {
			isParticipant = true
			break
		}
	}

	if !isParticipant {
		c.logger.Debug().
			Str("event_id", event.EventID).
			Str("party_id", c.partyID).
			Msg("not a participant, skipping event")
		return
	}

	// Determine if this node is the coordinator
	isCoordinator := c.isCoordinator(event.BlockNumber, participants)

	if isCoordinator {
		c.logger.Info().
			Str("event_id", event.EventID).
			Msg("acting as coordinator for event")
		// Update status to IN_PROGRESS
		if err := c.updateEventStatus(event.EventID, StatusInProgress, ""); err != nil {
			c.logger.Error().Err(err).Str("event_id", event.EventID).Msg("failed to update event status")
		}
	}

	// Parse event data
	var eventData EventData
	if len(event.EventData) > 0 {
		if err := json.Unmarshal(event.EventData, &eventData); err != nil {
			c.logger.Error().
				Err(err).
				Str("event_id", event.EventID).
				Msg("failed to parse event data")
			c.updateEventStatus(event.EventID, StatusFailed, fmt.Sprintf("failed to parse event data: %v", err))
			return
		}
	}

	// Pre-register session for this node to ensure it's ready to receive messages
	// This is important so that when the coordinator broadcasts, this node's session exists
	c.mu.RLock()
	service := c.service
	c.mu.RUnlock()
	if service == nil {
		c.logger.Error().Str("event_id", event.EventID).Msg("service not set, cannot process event")
		c.updateEventStatus(event.EventID, StatusFailed, "service not initialized")
		return
	}
	protocolType := tss.ProtocolType(event.ProtocolType)
	if err := service.RegisterSessionForEvent(protocolType, event.EventID, event.BlockNumber, participants); err != nil {
		c.logger.Warn().
			Err(err).
			Str("event_id", event.EventID).
			Msg("failed to pre-register session (may already exist)")
		// Continue anyway - session might already exist
	}

	// Execute the TSS operation
	var resultErr error
	switch event.ProtocolType {
	case string(tss.ProtocolKeygen):
		resultErr = c.handleKeygen(eventCtx, event, eventData, participants)
	case string(tss.ProtocolKeyrefresh):
		resultErr = c.handleKeyrefresh(eventCtx, event, eventData, participants)
	case string(tss.ProtocolSign):
		resultErr = c.handleSign(eventCtx, event, eventData, participants)
	default:
		resultErr = fmt.Errorf("unknown protocol type: %s", event.ProtocolType)
	}

	if resultErr != nil {
		c.logger.Error().
			Err(resultErr).
			Str("event_id", event.EventID).
			Msg("TSS operation failed")
		c.updateEventStatus(event.EventID, StatusFailed, resultErr.Error())
	} else {
		c.logger.Info().
			Str("event_id", event.EventID).
			Msg("TSS operation completed successfully")
		c.updateEventStatus(event.EventID, StatusSuccess, "")
	}
}

func (c *Coordinator) isCoordinator(blockNumber uint64, participants []*tss.UniversalValidator) bool {
	if len(participants) == 0 {
		return false
	}
	epoch := blockNumber / c.coordinatorRange
	idx := int(epoch % uint64(len(participants)))
	if idx >= len(participants) {
		return false
	}
	return participants[idx].PartyID() == c.partyID
}

func (c *Coordinator) handleKeygen(ctx context.Context, event store.TSSEvent, eventData EventData, participants []*tss.UniversalValidator) error {
	c.mu.RLock()
	service := c.service
	c.mu.RUnlock()
	if service == nil {
		return errors.New("service not initialized")
	}

	// Calculate threshold as > 2/3 of participants
	threshold := calculateThreshold(len(participants))

	req := core.KeygenRequest{
		EventID:      event.EventID,
		KeyID:        eventData.KeyID,
		Threshold:    threshold,
		BlockNumber:  event.BlockNumber,
		Participants: participants,
	}

	_, err := service.RunKeygen(ctx, req)
	return err
}

func (c *Coordinator) handleKeyrefresh(ctx context.Context, event store.TSSEvent, eventData EventData, participants []*tss.UniversalValidator) error {
	c.mu.RLock()
	service := c.service
	c.mu.RUnlock()
	if service == nil {
		return errors.New("service not initialized")
	}

	// Calculate threshold as > 2/3 of participants
	threshold := calculateThreshold(len(participants))

	req := core.KeyrefreshRequest{
		EventID:      event.EventID,
		KeyID:        eventData.KeyID,
		Threshold:    threshold,
		BlockNumber:  event.BlockNumber,
		Participants: participants,
	}

	_, err := service.RunKeyrefresh(ctx, req)
	return err
}

func (c *Coordinator) handleSign(ctx context.Context, event store.TSSEvent, eventData EventData, participants []*tss.UniversalValidator) error {
	c.mu.RLock()
	service := c.service
	c.mu.RUnlock()
	if service == nil {
		return errors.New("service not initialized")
	}

	// Calculate threshold as > 2/3 of participants
	threshold := calculateThreshold(len(participants))

	req := core.SignRequest{
		EventID:      event.EventID,
		KeyID:        eventData.KeyID,
		Threshold:    threshold,
		MessageHash:  eventData.MessageHash,
		ChainPath:    eventData.ChainPath,
		BlockNumber:  event.BlockNumber,
		Participants: participants,
	}

	_, err := service.RunSign(ctx, req)
	return err
}

// GetEvent implements core.EventStore interface for session recovery.
func (c *Coordinator) GetEvent(eventID string) (*core.EventInfo, error) {
	var event store.TSSEvent
	if err := c.db.Where("event_id = ?", eventID).First(&event).Error; err != nil {
		return nil, err
	}

	// Get all universal validators and filter for active ones
	allValidators, err := c.dataProvider.GetUniversalValidators(context.Background())
	if err != nil {
		return nil, err
	}

	// Filter for active validators only
	var participants []*tss.UniversalValidator
	for _, v := range allValidators {
		if v.Status == tss.UVStatusActive {
			participants = append(participants, v)
		}
	}

	return &core.EventInfo{
		EventID:      event.EventID,
		BlockNumber:  event.BlockNumber,
		ProtocolType: event.ProtocolType,
		Status:       event.Status,
		Participants: participants,
	}, nil
}

// SetService updates the service reference (used when service is recreated with EventStore).
func (c *Coordinator) SetService(service *core.Service) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.service = service
}

func (c *Coordinator) updateEventStatus(eventID, status, errorMsg string) error {
	update := map[string]interface{}{
		"status": status,
	}
	if errorMsg != "" {
		update["error_msg"] = errorMsg
	}

	result := c.db.Model(&store.TSSEvent{}).
		Where("event_id = ?", eventID).
		Updates(update)

	if result.Error != nil {
		return errors.Wrapf(result.Error, "failed to update event %s status to %s", eventID, status)
	}

	if result.RowsAffected == 0 {
		return errors.Errorf("event %s not found", eventID)
	}

	return nil
}

// EventData represents the parsed event data from the database.
type EventData struct {
	KeyID       string `json:"key_id"`
	MessageHash []byte `json:"message_hash,omitempty"`
	ChainPath   []byte `json:"chain_path,omitempty"`
}

// calculateThreshold calculates the threshold as > 2/3 of participants.
// Formula: threshold = floor((2 * n) / 3) + 1
// This ensures threshold > 2/3 * n
// Examples: 3->3, 4->3, 5->4, 6->5, 7->5, 8->6, 9->7
func calculateThreshold(numParticipants int) int {
	if numParticipants <= 0 {
		return 1
	}
	// Calculate > 2/3: floor((2 * n) / 3) + 1
	// This ensures we need more than 2/3 of participants
	threshold := (2*numParticipants)/3 + 1
	if threshold > numParticipants {
		threshold = numParticipants
	}
	return threshold
}
