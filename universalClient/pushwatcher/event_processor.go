package pushwatcher

import (
	"context"
	"sync"
	"time"

	"github.com/pushchain/push-chain-node/universalClient/db"
	"github.com/pushchain/push-chain-node/universalClient/externalchains/common"
	"github.com/pushchain/push-chain-node/universalClient/store"
	"github.com/rs/zerolog"
)

const eventProcessBatchSize = 1000

// EventHandler processes one CONFIRMED push chain event of a registered type.
// Handlers own the event's status transitions; a returned error is logged and
// the event is retried next tick.
type EventHandler interface {
	HandleEvent(ctx context.Context, event *store.Event) error
}

// EventProcessor drains CONFIRMED events from the push chain DB and dispatches
// them to the handler registered for their type. Event types without a handler
// are ignored (e.g. TSS events, which are consumed by the TSS subsystem).
type EventProcessor struct {
	chainStore *common.ChainStore
	handlers   map[string]EventHandler
	cfg        Config
	logger     zerolog.Logger

	mu      sync.Mutex
	running bool
	cancel  context.CancelFunc
	wg      sync.WaitGroup
}

// NewEventProcessor creates a new push event processor. Register handlers
// before Start.
func NewEventProcessor(
	database *db.DB,
	pollInterval time.Duration,
	logger zerolog.Logger,
) (*EventProcessor, error) {
	if database == nil {
		return nil, ErrNilDatabase
	}

	if pollInterval <= 0 {
		pollInterval = DefaultPollInterval
	}

	return &EventProcessor{
		chainStore: common.NewChainStore(database),
		handlers:   make(map[string]EventHandler),
		cfg:        Config{PollInterval: pollInterval},
		logger:     logger.With().Str("component", "push_event_processor").Logger(),
	}, nil
}

// RegisterHandler registers a handler for an event type. Must be called before Start.
func (p *EventProcessor) RegisterHandler(eventType string, handler EventHandler) {
	p.handlers[eventType] = handler
}

// Start begins processing events.
func (p *EventProcessor) Start(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.running {
		return ErrAlreadyRunning
	}

	childCtx, cancel := context.WithCancel(ctx)
	p.cancel = cancel
	p.running = true

	p.logger.Debug().
		Dur("poll_interval", p.cfg.PollInterval).
		Msg("starting push event processor")

	p.wg.Add(1)
	go p.run(childCtx)

	return nil
}

// Stop gracefully stops the processor.
func (p *EventProcessor) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.running {
		return ErrNotRunning
	}

	p.cancel()
	p.wg.Wait()
	p.running = false

	return nil
}

func (p *EventProcessor) run(ctx context.Context) {
	defer p.wg.Done()

	p.processConfirmedEvents(ctx)

	ticker := time.NewTicker(p.cfg.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.processConfirmedEvents(ctx)
		}
	}
}

// processConfirmedEvents dispatches CONFIRMED events to their registered handlers.
func (p *EventProcessor) processConfirmedEvents(ctx context.Context) {
	events, err := p.chainStore.GetConfirmedEvents(eventProcessBatchSize)
	if err != nil {
		p.logger.Error().Err(err).Msg("failed to query confirmed events")
		return
	}

	for _, event := range events {
		handler, ok := p.handlers[event.Type]
		if !ok {
			continue
		}

		select {
		case <-ctx.Done():
			return
		default:
		}

		if err := handler.HandleEvent(ctx, &event); err != nil {
			p.logger.Error().
				Err(err).
				Str("event_id", event.EventID).
				Str("type", event.Type).
				Msg("failed to process event")
		}
	}
}
