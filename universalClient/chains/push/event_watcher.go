package push

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/pushchain/push-chain-node/universalClient/pushcore"
	"github.com/pushchain/push-chain-node/universalClient/store"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

// Event queries for fetching specific event types from the chain.
const (
	TSSEventQuery      = EventTypeTSSProcessInitiated + ".process_id>=0"
	OutboundEventQuery = EventTypeOutboundCreated + ".tx_id EXISTS"
)

// EventWatcher polls the Push chain for events and stores them in the database.
// It handles graceful shutdown, concurrent safety, and persistent state tracking.
type EventWatcher struct {
	logger     zerolog.Logger
	pushClient PushClient
	db         *gorm.DB
	cfg        Config

	lastBlock atomic.Uint64
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	stopped   atomic.Bool
}

// NewEventWatcher creates a new event watcher.
func NewEventWatcher(
	client PushClient,
	db *gorm.DB,
	logger zerolog.Logger,
	cfg Config,
	startBlock uint64,
) *EventWatcher {
	w := &EventWatcher{
		logger:     logger.With().Str("component", "event_watcher").Logger(),
		pushClient: client,
		db:         db,
		cfg:        cfg,
	}
	w.lastBlock.Store(startBlock)
	return w
}

// Start begins the event watching loop.
// The watcher will continue until Stop is called or the context is cancelled.
func (w *EventWatcher) Start(ctx context.Context) error {
	w.ctx, w.cancel = context.WithCancel(ctx)
	w.stopped.Store(false)

	w.wg.Add(1)
	go w.watchLoop()

	return nil
}

// Stop gracefully stops the event watcher and waits for the watch loop to exit.
func (w *EventWatcher) Stop() {
	if w.stopped.Swap(true) {
		return // Already stopped
	}

	if w.cancel != nil {
		w.cancel()
	}

	// Wait for the watch loop to complete
	w.wg.Wait()
}

// LastProcessedBlock returns the last block number that was successfully processed.
func (w *EventWatcher) LastProcessedBlock() uint64 {
	return w.lastBlock.Load()
}

// watchLoop is the main polling loop that queries for Push chain events.
func (w *EventWatcher) watchLoop() {
	defer w.wg.Done()

	ticker := time.NewTicker(w.cfg.PollInterval)
	defer ticker.Stop()

	// Perform initial poll on startup
	if err := w.pollForEvents(); err != nil {
		w.logger.Error().Err(err).Msg("initial poll failed")
	}

	for {
		select {
		case <-w.ctx.Done():
			w.logger.Info().Msg("event watcher shutting down")
			return
		case <-ticker.C:
			if err := w.pollForEvents(); err != nil {
				w.logger.Error().Err(err).Msg("poll cycle failed")
			}
		}
	}
}

// pollForEvents queries the chain for new events and stores them.
// Processes blocks in configurable chunks to avoid overwhelming the chain.
func (w *EventWatcher) pollForEvents() error {
	latestBlock, err := w.pushClient.GetLatestBlock()
	if err != nil {
		return fmt.Errorf("failed to get latest block: %w", err)
	}

	currentBlock := w.lastBlock.Load()

	// Already caught up
	if currentBlock >= latestBlock {
		return nil
	}

	return w.processBlockRange(currentBlock, latestBlock)
}

// processBlockRange processes all blocks from start to end in chunks.
func (w *EventWatcher) processBlockRange(start, end uint64) error {
	processedBlock := start

	for processedBlock < end {
		// Check for cancellation between chunks
		select {
		case <-w.ctx.Done():
			return w.ctx.Err()
		default:
		}

		// Calculate chunk boundaries
		minHeight := processedBlock + 1
		maxHeight := min(processedBlock+w.cfg.ChunkSize, end)

		// Process this chunk
		newEvents, err := w.processChunk(minHeight, maxHeight)
		if err != nil {
			return fmt.Errorf("failed to process blocks %d-%d: %w", minHeight, maxHeight, err)
		}

		if newEvents > 0 {
			w.logger.Info().
				Int("new_events", newEvents).
				Uint64("from_block", minHeight).
				Uint64("to_block", maxHeight).
				Msg("processed events")
		}

		// Update state
		processedBlock = maxHeight
		w.lastBlock.Store(processedBlock)

		// Persist progress to database
		if err := w.persistBlockProgress(processedBlock); err != nil {
			w.logger.Error().
				Err(err).
				Uint64("block", processedBlock).
				Msg("failed to persist block progress")
			// Continue processing - state will be recovered on restart
		}
	}

	return nil
}

// processChunk processes a single chunk of blocks and returns the number of new events stored.
func (w *EventWatcher) processChunk(minHeight, maxHeight uint64) (int, error) {
	w.logger.Debug().
		Uint64("min_height", minHeight).
		Uint64("max_height", maxHeight).
		Msg("querying events")

	newEventsCount := 0

	// Query TSS events
	tssCount, err := w.queryAndStoreEvents(TSSEventQuery, minHeight, maxHeight, "TSS")
	if err != nil {
		return 0, fmt.Errorf("TSS query failed: %w", err)
	}
	newEventsCount += tssCount

	// Query outbound events
	outboundCount, err := w.queryAndStoreEvents(OutboundEventQuery, minHeight, maxHeight, "outbound")
	if err != nil {
		return 0, fmt.Errorf("outbound query failed: %w", err)
	}
	newEventsCount += outboundCount

	return newEventsCount, nil
}

// queryAndStoreEvents queries for events matching the query and stores them.
func (w *EventWatcher) queryAndStoreEvents(query string, minHeight, maxHeight uint64, eventType string) (int, error) {
	txResults, err := w.pushClient.GetTxsByEvents(
		query,
		minHeight,
		maxHeight,
		w.cfg.QueryLimit,
	)
	if err != nil {
		return 0, err
	}

	newEventsCount := 0
	for _, txResult := range txResults {
		events := w.extractEventsFromTx(txResult)
		for _, event := range events {
			if stored, err := w.storeEvent(event); err != nil {
				w.logger.Error().
					Err(err).
					Str("event_id", event.EventID).
					Str("event_type", eventType).
					Msg("failed to store event")
			} else if stored {
				newEventsCount++
			}
		}
	}

	return newEventsCount, nil
}

// extractEventsFromTx extracts Push chain events from a transaction result.
func (w *EventWatcher) extractEventsFromTx(txResult *pushcore.TxResult) []*store.PCEvent {
	if txResult == nil || txResult.TxResponse == nil || txResult.TxResponse.TxResponse == nil {
		return nil
	}

	txResp := txResult.TxResponse.TxResponse
	blockHeight := uint64(txResult.Height)
	txHash := txResult.TxHash

	events := make([]*store.PCEvent, 0, len(txResp.Events))

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
			w.logger.Warn().
				Err(err).
				Str("tx_hash", txHash).
				Str("event_type", evt.Type).
				Msg("failed to parse event")
			continue
		}

		if parsed != nil {
			parsed.TxHash = txHash
			events = append(events, parsed)
		}
	}

	return events
}

// storeEvent stores a Push chain event in the database if it doesn't already exist.
// Returns (true, nil) if a new event was stored, (false, nil) if it already existed,
// or (false, error) if storage failed.
func (w *EventWatcher) storeEvent(event *store.PCEvent) (bool, error) {
	// Check for existing event
	var existing store.PCEvent
	err := w.db.Where("event_id = ?", event.EventID).First(&existing).Error
	if err == nil {
		// Event already exists
		return false, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return false, fmt.Errorf("failed to check existing event: %w", err)
	}

	// Store new event
	if err := w.db.Create(event).Error; err != nil {
		return false, fmt.Errorf("failed to create event: %w", err)
	}

	w.logger.Debug().
		Str("event_id", event.EventID).
		Str("type", event.Type).
		Uint64("block_height", event.BlockHeight).
		Msg("stored new event")

	return true, nil
}

// persistBlockProgress updates the last processed block in chain_states.
func (w *EventWatcher) persistBlockProgress(blockNumber uint64) error {
	var chainState store.ChainState

	err := w.db.First(&chainState).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("failed to query chain state: %w", err)
	}

	if errors.Is(err, gorm.ErrRecordNotFound) {
		// Create new record
		chainState = store.ChainState{LastBlock: blockNumber}
		if err := w.db.Create(&chainState).Error; err != nil {
			return fmt.Errorf("failed to create chain state: %w", err)
		}
		return nil
	}

	// Update existing record if we've progressed
	if blockNumber > chainState.LastBlock {
		chainState.LastBlock = blockNumber
		if err := w.db.Save(&chainState).Error; err != nil {
			return fmt.Errorf("failed to update chain state: %w", err)
		}
	}

	return nil
}

// min returns the smaller of two uint64 values.
func min(a, b uint64) uint64 {
	if a < b {
		return a
	}
	return b
}
