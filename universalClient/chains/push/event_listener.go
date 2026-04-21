package push

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/pushchain/push-chain-node/universalClient/chains/common"
	"github.com/pushchain/push-chain-node/universalClient/config"
	"github.com/pushchain/push-chain-node/universalClient/db"
	"github.com/pushchain/push-chain-node/universalClient/pushcore"
	"github.com/pushchain/push-chain-node/universalClient/store"
	"github.com/rs/zerolog"
)

const DefaultPollInterval = 2 * time.Second

var (
	ErrNilClient      = errors.New("push client is nil")
	ErrNilDatabase    = errors.New("database is nil")
	ErrAlreadyRunning = errors.New("event listener is already running")
	ErrNotRunning     = errors.New("event listener is not running")
)

// Config holds configuration for the Push event listener.
type Config struct {
	PollInterval time.Duration
}

// EventListener polls Push chain for active TSS events and pending outbounds
// via gRPC, converts them to store.Events, and inserts them into the local DB.
type EventListener struct {
	pushCore   *pushcore.Client
	chainStore *common.ChainStore
	cfg        Config
	logger     zerolog.Logger

	mu      sync.Mutex
	running bool
	cancel  context.CancelFunc
	wg      sync.WaitGroup
}

// NewEventListener creates a new Push event listener.
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

	pollInterval := DefaultPollInterval
	if chainConfig != nil && chainConfig.EventPollingIntervalSeconds != nil && *chainConfig.EventPollingIntervalSeconds > 0 {
		pollInterval = time.Duration(*chainConfig.EventPollingIntervalSeconds) * time.Second
	}

	return &EventListener{
		pushCore:   pushCore,
		chainStore: common.NewChainStore(database),
		cfg:        Config{PollInterval: pollInterval},
		logger:     logger.With().Str("component", "push_event_listener").Logger(),
	}, nil
}

// Start begins polling for Push chain events.
func (el *EventListener) Start(ctx context.Context) error {
	el.mu.Lock()
	defer el.mu.Unlock()

	if el.running {
		return ErrAlreadyRunning
	}

	childCtx, cancel := context.WithCancel(ctx)
	el.cancel = cancel
	el.running = true

	el.logger.Info().
		Dur("poll_interval", el.cfg.PollInterval).
		Msg("starting Push event listener")

	el.wg.Add(1)
	go el.run(childCtx)

	return nil
}

// Stop gracefully stops the event listener.
func (el *EventListener) Stop() error {
	el.mu.Lock()
	defer el.mu.Unlock()

	if !el.running {
		return ErrNotRunning
	}

	el.cancel()
	el.wg.Wait()
	el.running = false

	el.logger.Info().Msg("Push event listener stopped")
	return nil
}

// IsRunning returns whether the event listener is currently running.
func (el *EventListener) IsRunning() bool {
	el.mu.Lock()
	defer el.mu.Unlock()
	return el.running
}

// run is the main loop: poll immediately, then on every tick.
func (el *EventListener) run(ctx context.Context) {
	defer el.wg.Done()

	el.poll(ctx)

	ticker := time.NewTicker(el.cfg.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			el.poll(ctx)
		}
	}
}

// poll fetches pending TSS, outbound & fund migration events, stores them, and updates latest block height.
func (el *EventListener) poll(ctx context.Context) {
	tssCount := el.pollTssEvents(ctx)
	outboundCount := el.pollOutboundEvents(ctx)
	migrationCount := el.pollFundMigrationEvents(ctx)

	if total := tssCount + outboundCount + migrationCount; total > 0 {
		el.logger.Info().
			Int("tss_events", tssCount).
			Int("outbound_events", outboundCount).
			Int("migration_events", migrationCount).
			Msg("stored new events")
	}

	// Update chain height to latest block
	latestBlock, err := el.pushCore.GetLatestBlock(ctx)
	if err != nil {
		el.logger.Error().Err(err).Msg("failed to get latest block height")
		return
	}
	if err := el.chainStore.UpdateChainHeight(latestBlock); err != nil {
		el.logger.Error().Err(err).Uint64("height", latestBlock).Msg("failed to persist chain height")
	}
}

// pollTssEvents fetches pending TSS events and inserts them into the DB. Returns new event count.
func (el *EventListener) pollTssEvents(ctx context.Context) int {
	tssEvents, err := el.pushCore.GetPendingTssEvents(ctx)
	if err != nil {
		el.logger.Error().Err(err).Msg("failed to fetch pending TSS events")
		return 0
	}

	var newCount int
	for _, te := range tssEvents {
		event, err := convertTssEvent(te)
		if err != nil {
			el.logger.Warn().Err(err).Uint64("process_id", te.ProcessId).Msg("failed to convert TSS event")
			continue
		}

		newCount += el.storeEvent(event)
	}

	return newCount
}

// pollOutboundEvents fetches pending outbounds and inserts them into the DB.
// Returns new event count.
func (el *EventListener) pollOutboundEvents(ctx context.Context) int {
	entries, outbounds, err := el.pushCore.GetAllPendingOutbounds(ctx)
	if err != nil {
		el.logger.Error().Err(err).Msg("failed to fetch pending outbounds")
		return 0
	}

	if len(entries) != len(outbounds) {
		el.logger.Error().
			Int("entries", len(entries)).
			Int("outbounds", len(outbounds)).
			Msg("mismatched entries and outbounds lengths")
		return 0
	}

	var newCount int
	for i, entry := range entries {
		event, err := convertOutboundToEvent(entry, outbounds[i])
		if err != nil {
			el.logger.Warn().Err(err).Str("outbound_id", entry.OutboundId).Msg("failed to convert outbound event")
			continue
		}

		newCount += el.storeEvent(event)
	}

	return newCount
}

// pollFundMigrationEvents fetches pending fund migrations and inserts them into the DB.
// Returns new event count.
func (el *EventListener) pollFundMigrationEvents(ctx context.Context) int {
	migrations, err := el.pushCore.GetPendingFundMigrations(ctx)
	if err != nil {
		el.logger.Error().Err(err).Msg("failed to fetch pending fund migrations")
		return 0
	}

	var newCount int
	for _, m := range migrations {
		event, err := convertFundMigrationEvent(m)
		if err != nil {
			el.logger.Warn().Err(err).Uint64("migration_id", m.Id).Msg("failed to convert fund migration event")
			continue
		}

		newCount += el.storeEvent(event)
	}

	return newCount
}

// storeEvent inserts an event into the DB if it doesn't already exist.
// Returns 1 if stored, 0 if duplicate or error.
func (el *EventListener) storeEvent(event *store.Event) int {
	stored, err := el.chainStore.InsertEventIfNotExists(event)
	if err != nil {
		el.logger.Error().Err(err).Str("event_id", event.EventID).Msg("failed to store event")
		return 0
	}
	if stored {
		el.logger.Debug().
			Str("event_id", event.EventID).
			Str("type", event.Type).
			Uint64("block_height", event.BlockHeight).
			Msg("stored new event")
		return 1
	}
	return 0
}
