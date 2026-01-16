package push

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/pushchain/push-chain-node/universalClient/chains/common"
	"github.com/pushchain/push-chain-node/universalClient/config"
	"github.com/pushchain/push-chain-node/universalClient/db"
	"github.com/pushchain/push-chain-node/universalClient/pushcore"
	"github.com/pushchain/push-chain-node/universalClient/store"
	"github.com/rs/zerolog"
)

// Config holds configuration for the Push event listener
type Config struct {
	PollInterval time.Duration
	ChunkSize    uint64
	QueryLimit   uint64
}

// Default configuration values
const (
	DefaultPollInterval = 5 * time.Second
	DefaultChunkSize    = 1000
	DefaultQueryLimit   = 100
)

// Validation constraints
const (
	minPollInterval = 1 * time.Second
	maxPollInterval = 5 * time.Minute
)

// Event queries for fetching specific event types from the chain.
const (
	TSSEventQuery      = EventTypeTSSProcessInitiated + ".process_id>=0"
	OutboundEventQuery = EventTypeOutboundCreated + ".tx_id EXISTS"
)

// Errors
var (
	ErrNilClient      = errors.New("push client is nil")
	ErrNilDatabase    = errors.New("database is nil")
	ErrAlreadyRunning = errors.New("event listener is already running")
	ErrNotRunning     = errors.New("event listener is not running")
)

// Validate validates the configuration
func (c *Config) Validate() error {
	if c.PollInterval < minPollInterval {
		return errors.New("poll interval is too short (minimum 1 second)")
	}
	if c.PollInterval > maxPollInterval {
		return errors.New("poll interval is too long (maximum 5 minutes)")
	}
	return nil
}

// applyDefaults applies default values to zero fields
func (c *Config) applyDefaults() {
	if c.PollInterval == 0 {
		c.PollInterval = DefaultPollInterval
	}
	if c.ChunkSize == 0 {
		c.ChunkSize = DefaultChunkSize
	}
	if c.QueryLimit == 0 {
		c.QueryLimit = DefaultQueryLimit
	}
}

// EventListener listens for events from the Push chain and stores them in the database
type EventListener struct {
	pushCore    *pushcore.Client
	database    *db.DB
	chainStore  *common.ChainStore
	cfg         Config
	chainConfig *config.ChainSpecificConfig
	logger      zerolog.Logger
	mu          sync.RWMutex
	stopCh      chan struct{}
	running     bool

	// Event watching state
	lastBlock atomic.Uint64
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	stopped   atomic.Bool
}

// NewEventListener creates a new Push event listener
func NewEventListener(
	pushCore *pushcore.Client,
	database *db.DB,
	logger zerolog.Logger,
	chainConfig *config.ChainSpecificConfig,
) (*EventListener, error) {
	if pushCore == nil {
		return nil, ErrNilClient
	}
	if database == nil {
		return nil, ErrNilDatabase
	}

	// Create config from chainConfig
	cfg := &Config{
		PollInterval: DefaultPollInterval,
		ChunkSize:    DefaultChunkSize,
		QueryLimit:   DefaultQueryLimit,
	}

	// Override with chainConfig if available
	if chainConfig != nil {
		if chainConfig.EventPollingIntervalSeconds != nil && *chainConfig.EventPollingIntervalSeconds > 0 {
			cfg.PollInterval = time.Duration(*chainConfig.EventPollingIntervalSeconds) * time.Second
		}
	}

	// Apply defaults first
	cfg.applyDefaults()

	// Then validate
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	// Create chain store
	chainStore := common.NewChainStore(database)

	return &EventListener{
		pushCore:    pushCore,
		database:    database,
		chainStore:  chainStore,
		cfg:         *cfg,
		chainConfig: chainConfig,
		logger:      logger.With().Str("component", "push_event_listener").Logger(),
		stopCh:      make(chan struct{}),
	}, nil
}

// Start begins listening for events from the Push chain
func (el *EventListener) Start(ctx context.Context) error {
	if el.running {
		return ErrAlreadyRunning
	}

	el.mu.Lock()
	defer el.mu.Unlock()

	el.running = true

	// Load last processed block from chain_states
	startBlock, err := el.getLastProcessedBlock(ctx)
	if err != nil {
		el.running = false
		return fmt.Errorf("failed to get last processed block: %w", err)
	}

	el.logger.Info().
		Uint64("start_block", startBlock).
		Dur("poll_interval", el.cfg.PollInterval).
		Uint64("chunk_size", el.cfg.ChunkSize).
		Msg("starting Push event listener")

	// Reset stop channel for new run
	el.stopCh = make(chan struct{})
	el.lastBlock.Store(startBlock)
	el.ctx, el.cancel = context.WithCancel(ctx)
	el.stopped.Store(false)

	// Start the watch loop
	el.wg.Add(1)
	go el.watchLoop()

	el.logger.Info().Msg("Push event listener started successfully")
	return nil
}

// IsRunning returns whether the event listener is currently running
func (el *EventListener) IsRunning() bool {
	return el.running
}

// Stop gracefully stops the event listener
func (el *EventListener) Stop() error {
	if !el.running {
		return ErrNotRunning
	}

	el.mu.Lock()
	defer el.mu.Unlock()

	el.logger.Info().Msg("stopping Push event listener")

	// Signal stop
	close(el.stopCh)

	// Stop the watch loop
	if el.stopped.Swap(true) {
		// Already stopped
		el.running = false
		return nil
	}

	if el.cancel != nil {
		el.cancel()
	}

	// Wait for the watch loop to complete
	el.wg.Wait()

	el.running = false
	el.logger.Info().Msg("Push event listener stopped successfully")
	return nil
}

// getLastProcessedBlock reads the last processed block from chain_states using chainStore
// If DB state is empty and EventStartFrom is configured, uses that as the starting block
func (el *EventListener) getLastProcessedBlock(ctx context.Context) (uint64, error) {
	// Get chain height from store
	blockHeight, err := el.chainStore.GetChainHeight()
	if err != nil {
		return 0, fmt.Errorf("failed to get chain height: %w", err)
	}

	// If no previous state or invalid, check config
	if blockHeight == 0 {
		return el.getStartBlockFromConfig(ctx)
	}

	el.logger.Info().
		Uint64("block", blockHeight).
		Msg("resuming from last processed block")

	return blockHeight, nil
}

// getStartBlockFromConfig determines start block from configuration
func (el *EventListener) getStartBlockFromConfig(ctx context.Context) (uint64, error) {
	// Check config for EventStartFrom
	if el.chainConfig != nil && el.chainConfig.EventStartFrom != nil {
		if *el.chainConfig.EventStartFrom >= 0 {
			startBlock := uint64(*el.chainConfig.EventStartFrom)
			el.logger.Info().
				Uint64("block", startBlock).
				Msg("no previous state found, starting from configured EventStartFrom")
			return startBlock, nil
		}

		// -1 means start from latest block
		if *el.chainConfig.EventStartFrom == -1 {
			latestBlock, err := el.pushCore.GetLatestBlock(ctx)
			if err != nil {
				el.logger.Warn().Err(err).Msg("failed to get latest block, starting from 0")
				return 0, nil
			}
			el.logger.Info().
				Uint64("block", latestBlock).
				Msg("no previous state found, starting from latest block (EventStartFrom=-1)")
			return latestBlock, nil
		}
	}

	el.logger.Info().Msg("no previous state found, starting from block 0")
	return 0, nil
}

// watchLoop is the main polling loop that queries for Push chain events.
func (el *EventListener) watchLoop() {
	defer el.wg.Done()

	ticker := time.NewTicker(el.cfg.PollInterval)
	defer ticker.Stop()

	// Perform initial poll on startup
	if err := el.pollForEvents(el.ctx); err != nil {
		el.logger.Error().Err(err).Msg("initial poll failed")
	}

	for {
		select {
		case <-el.ctx.Done():
			el.logger.Info().Msg("event listener shutting down")
			return
		case <-el.stopCh:
			el.logger.Info().Msg("stop signal received, shutting down")
			return
		case <-ticker.C:
			if err := el.pollForEvents(el.ctx); err != nil {
				el.logger.Error().Err(err).Msg("poll cycle failed")
			}
		}
	}
}

// pollForEvents queries the chain for new events and stores them.
// Processes blocks in configurable chunks to avoid overwhelming the chain.
func (el *EventListener) pollForEvents(ctx context.Context) error {
	latestBlock, err := el.pushCore.GetLatestBlock(ctx)
	if err != nil {
		return fmt.Errorf("failed to get latest block: %w", err)
	}

	currentBlock := el.lastBlock.Load()

	// Already caught up
	if currentBlock >= latestBlock {
		return nil
	}

	return el.processBlockRange(ctx, currentBlock, latestBlock)
}

// processBlockRange processes all blocks from start to end in chunks.
func (el *EventListener) processBlockRange(ctx context.Context, start, end uint64) error {
	processedBlock := start

	for processedBlock < end {
		// Check for cancellation between chunks
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-el.stopCh:
			return fmt.Errorf("stop signal received")
		default:
		}

		// Calculate chunk boundaries
		minHeight := processedBlock + 1
		maxHeight := min(processedBlock+el.cfg.ChunkSize, end)

		// Process this chunk
		newEvents, err := el.processChunk(ctx, minHeight, maxHeight)
		if err != nil {
			return fmt.Errorf("failed to process blocks %d-%d: %w", minHeight, maxHeight, err)
		}

		if newEvents > 0 {
			el.logger.Info().
				Int("new_events", newEvents).
				Uint64("from_block", minHeight).
				Uint64("to_block", maxHeight).
				Msg("processed events")
		}

		// Update state
		processedBlock = maxHeight
		el.lastBlock.Store(processedBlock)

		// Persist progress to database using chainStore
		if err := el.chainStore.UpdateChainHeight(processedBlock); err != nil {
			el.logger.Error().
				Err(err).
				Uint64("block", processedBlock).
				Msg("failed to persist block progress")
			// Continue processing - state will be recovered on restart
		}
	}

	return nil
}

// processChunk processes a single chunk of blocks and returns the number of new events stored.
func (el *EventListener) processChunk(ctx context.Context, minHeight, maxHeight uint64) (int, error) {
	el.logger.Debug().
		Uint64("min_height", minHeight).
		Uint64("max_height", maxHeight).
		Msg("querying events")

	newEventsCount := 0

	// Query TSS events
	tssCount, err := el.queryAndStoreEvents(ctx, TSSEventQuery, minHeight, maxHeight, "TSS")
	if err != nil {
		return 0, fmt.Errorf("TSS query failed: %w", err)
	}
	newEventsCount += tssCount

	// Query outbound events
	outboundCount, err := el.queryAndStoreEvents(ctx, OutboundEventQuery, minHeight, maxHeight, "outbound")
	if err != nil {
		return 0, fmt.Errorf("outbound query failed: %w", err)
	}
	newEventsCount += outboundCount

	return newEventsCount, nil
}

// queryAndStoreEvents queries for events matching the query and stores them.
func (el *EventListener) queryAndStoreEvents(ctx context.Context, query string, minHeight, maxHeight uint64, eventType string) (int, error) {
	txResults, err := el.pushCore.GetTxsByEvents(
		ctx,
		query,
		minHeight,
		maxHeight,
		el.cfg.QueryLimit,
	)
	if err != nil {
		return 0, err
	}

	newEventsCount := 0
	for _, txResult := range txResults {
		events := el.extractEventsFromTx(txResult)
		for _, event := range events {
			if stored, err := el.chainStore.InsertEventIfNotExists(event); err != nil {
				el.logger.Error().
					Err(err).
					Str("event_id", event.EventID).
					Str("event_type", eventType).
					Msg("failed to store event")
			} else if stored {
				newEventsCount++
				el.logger.Debug().
					Str("event_id", event.EventID).
					Str("type", event.Type).
					Uint64("block_height", event.BlockHeight).
					Msg("stored new event")
			}
		}
	}

	return newEventsCount, nil
}

// extractEventsFromTx extracts Push chain events from a transaction result.
func (el *EventListener) extractEventsFromTx(txResult *pushcore.TxResult) []*store.Event {
	if txResult == nil || txResult.TxResponse == nil || txResult.TxResponse.TxResponse == nil {
		return nil
	}

	txResp := txResult.TxResponse.TxResponse
	blockHeight := uint64(txResult.Height)
	txHash := txResult.TxHash

	events := make([]*store.Event, 0, len(txResp.Events))

	for _, evt := range txResp.Events {
		// Convert SDK event attributes to ABCI format
		attrs := make([]abci.EventAttribute, 0, len(evt.Attributes))
		for _, attr := range evt.Attributes {
			attrs = append(attrs, abci.EventAttribute{
				Key:   attr.Key,
				Value: attr.Value,
			})
		}

		abciEvent := abci.Event{
			Type:       evt.Type,
			Attributes: attrs,
		}

		parsed, err := ParseEvent(abciEvent, blockHeight)
		if err != nil {
			el.logger.Warn().
				Err(err).
				Str("tx_hash", txHash).
				Str("event_type", evt.Type).
				Msg("failed to parse event")
			continue
		}

		if parsed != nil {
			events = append(events, parsed)
		}
	}

	return events
}

// min returns the smaller of two uint64 values.
func min(a, b uint64) uint64 {
	if a < b {
		return a
	}
	return b
}
