package push

import (
	"context"
	"time"

	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/pushchain/push-chain-node/universalClient/pushcore"
	"github.com/pushchain/push-chain-node/universalClient/tss/eventstore"
	"github.com/rs/zerolog"
)

// EventWatcher polls the Push chain for TSS events and stores them in the database.
type EventWatcher struct {
	logger       zerolog.Logger
	pushClient   *pushcore.Client
	eventStore   *eventstore.Store
	pollInterval time.Duration
	lastBlock    uint64

	ctx    context.Context
	cancel context.CancelFunc
}

// NewEventWatcher creates a new event watcher.
func NewEventWatcher(
	client *pushcore.Client,
	store *eventstore.Store,
	logger zerolog.Logger,
) *EventWatcher {
	return &EventWatcher{
		logger:       logger.With().Str("component", "push_event_watcher").Logger(),
		pushClient:   client,
		eventStore:   store,
		pollInterval: DefaultPollInterval,
		lastBlock:    0,
	}
}

// SetPollInterval sets the polling interval.
func (w *EventWatcher) SetPollInterval(interval time.Duration) {
	w.pollInterval = interval
}

// SetLastBlock sets the starting block for polling.
func (w *EventWatcher) SetLastBlock(block uint64) {
	w.lastBlock = block
}

// Start begins the event watching loop.
func (w *EventWatcher) Start(ctx context.Context) {
	w.ctx, w.cancel = context.WithCancel(ctx)
	go w.watchLoop()
}

// Stop stops the event watching loop.
func (w *EventWatcher) Stop() {
	if w.cancel != nil {
		w.cancel()
	}
}

// watchLoop is the main polling loop that queries for TSS events.
func (w *EventWatcher) watchLoop() {
	ticker := time.NewTicker(w.pollInterval)
	defer ticker.Stop()

	// Initial poll
	w.pollForEvents()

	for {
		select {
		case <-w.ctx.Done():
			w.logger.Info().Msg("event watcher stopped")
			return
		case <-ticker.C:
			w.pollForEvents()
		}
	}
}

// pollForEvents queries the chain for new TSS events.
func (w *EventWatcher) pollForEvents() {
	// Get the latest block number
	latestBlock, err := w.pushClient.GetLatestBlockNum()
	if err != nil {
		w.logger.Error().Err(err).Msg("failed to get latest block number")
		return
	}

	// Skip if we're already caught up
	if w.lastBlock >= latestBlock {
		return
	}

	// Query for TSS events since the last processed block
	minHeight := w.lastBlock + 1
	if w.lastBlock == 0 {
		// First run - only get events from recent blocks to avoid scanning entire chain
		if latestBlock > 1000 {
			minHeight = latestBlock - 1000
		} else {
			minHeight = 1
		}
	}

	w.logger.Debug().
		Uint64("min_height", minHeight).
		Uint64("max_height", latestBlock).
		Msg("polling for TSS events")

	// Query transactions with tss_process_initiated events
	txResults, err := w.pushClient.GetTxsByEvents(
		DefaultEventQuery,
		minHeight,
		latestBlock,
		100, // limit
	)
	if err != nil {
		w.logger.Error().Err(err).Msg("failed to query TSS events")
		return
	}

	// Process each transaction
	newEventsCount := 0
	for _, txResult := range txResults {
		events := w.extractEventsFromTx(txResult)
		for _, event := range events {
			if w.storeEvent(event) {
				newEventsCount++
			}
		}
	}

	if newEventsCount > 0 {
		w.logger.Info().
			Int("new_events", newEventsCount).
			Uint64("from_block", minHeight).
			Uint64("to_block", latestBlock).
			Msg("processed TSS events")
	}

	// Update the last processed block
	w.lastBlock = latestBlock
}

// extractEventsFromTx extracts TSS events from a transaction result.
func (w *EventWatcher) extractEventsFromTx(txResult *pushcore.TxResult) []*TSSProcessEvent {
	if txResult == nil || txResult.TxResponse == nil || txResult.TxResponse.TxResponse == nil {
		return nil
	}

	var events []*TSSProcessEvent

	// Get events from the transaction response
	txResp := txResult.TxResponse.TxResponse

	// Convert SDK events to ABCI events for parsing
	abciEvents := make([]abci.Event, 0, len(txResp.Events))
	for _, evt := range txResp.Events {
		attrs := make([]abci.EventAttribute, 0, len(evt.Attributes))
		for _, attr := range evt.Attributes {
			attrs = append(attrs, abci.EventAttribute{
				Key:   attr.Key,
				Value: attr.Value,
			})
		}
		abciEvents = append(abciEvents, abci.Event{
			Type:       evt.Type,
			Attributes: attrs,
		})
	}

	// Parse TSS events
	parsed, err := ParseTSSProcessInitiatedEvent(abciEvents, uint64(txResult.Height), txResult.TxHash)
	if err != nil {
		w.logger.Warn().
			Err(err).
			Str("tx_hash", txResult.TxHash).
			Msg("failed to parse TSS event")
		return nil
	}

	if parsed != nil {
		events = append(events, parsed)
	}

	return events
}

// storeEvent stores a TSS event in the database if it doesn't already exist.
// Returns true if a new event was stored, false if it already existed.
func (w *EventWatcher) storeEvent(event *TSSProcessEvent) bool {
	eventID := event.EventID()

	// Check if event already exists
	existing, err := w.eventStore.GetEvent(eventID)
	if err == nil && existing != nil {
		return false
	}

	// Use eventstore to create
	record := event.ToTSSEventRecord()
	if err := w.eventStore.CreateEvent(record); err != nil {
		w.logger.Error().Err(err).Str("event_id", eventID).Msg("failed to store TSS event")
		return false
	}
	return true
}

// GetLastBlock returns the last processed block height.
func (w *EventWatcher) GetLastBlock() uint64 {
	return w.lastBlock
}
