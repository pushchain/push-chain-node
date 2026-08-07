package common

import (
	"context"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/mr-tron/base58"
	"github.com/pushchain/push-chain-node/universalClient/db"
	"github.com/pushchain/push-chain-node/universalClient/store"
	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
	"github.com/rs/zerolog"
)

const eventProcessBatchSize = 1000

// VoteSigner is the subset of pushsigner.Signer used by the event processors.
// Defined here (consumer-side) so tests can provide mock implementations.
type VoteSigner interface {
	VoteInbound(ctx context.Context, inbound *uexecutortypes.Inbound) (string, error)
	VoteOutbound(ctx context.Context, txID string, utxID string, observation *uexecutortypes.OutboundObservation) (string, error)
}

// EventHandler processes one CONFIRMED event of a registered type.
// Handlers own the event's status transitions; a returned error is logged and
// the event is retried next tick.
type EventHandler interface {
	HandleEvent(ctx context.Context, event *store.Event) error
}

// EventProcessor drains CONFIRMED events from the chain's database and
// dispatches them to the handler registered for their type. Event types
// without a handler are ignored.
type EventProcessor struct {
	chainStore *ChainStore
	handlers   map[string]EventHandler
	chainID    string
	logger     zerolog.Logger
	running    bool
	stopCh     chan struct{}
	wg         sync.WaitGroup
}

// NewEventProcessor creates a new event processor. Register handlers before Start.
func NewEventProcessor(
	database *db.DB,
	chainID string,
	logger zerolog.Logger,
) *EventProcessor {
	return &EventProcessor{
		chainStore: NewChainStore(database),
		handlers:   make(map[string]EventHandler),
		chainID:    chainID,
		logger:     logger.With().Str("component", "event_processor").Str("chain", chainID).Logger(),
		stopCh:     make(chan struct{}),
	}
}

// RegisterHandler registers a handler for an event type. Must be called before Start.
func (ep *EventProcessor) RegisterHandler(eventType string, handler EventHandler) {
	ep.handlers[eventType] = handler
}

// Start begins processing events
func (ep *EventProcessor) Start(ctx context.Context) error {
	if ep.running {
		return fmt.Errorf("event processor is already running")
	}

	ep.running = true
	ep.stopCh = make(chan struct{})

	ep.wg.Add(1)
	go ep.processLoop(ctx)

	ep.logger.Info().Msg("event processor started")
	return nil
}

// Stop gracefully stops the event processor
func (ep *EventProcessor) Stop() error {
	if !ep.running {
		return nil
	}

	ep.logger.Debug().Msg("stopping event processor")
	close(ep.stopCh)
	ep.running = false

	ep.wg.Wait()
	ep.logger.Info().Msg("event processor stopped")
	return nil
}

// IsRunning returns whether the processor is currently running
func (ep *EventProcessor) IsRunning() bool {
	return ep.running
}

// processLoop is the main event processing loop
func (ep *EventProcessor) processLoop(ctx context.Context) {
	defer ep.wg.Done()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			ep.logger.Debug().Msg("context cancelled, stopping event processor")
			return
		case <-ep.stopCh:
			ep.logger.Debug().Msg("stop signal received, stopping event processor")
			return
		case <-ticker.C:
			if err := ep.processConfirmedEvents(ctx); err != nil {
				ep.logger.Error().Err(err).Msg("failed to process confirmed events")
			}
		}
	}
}

// processConfirmedEvents dispatches CONFIRMED events to their registered handlers.
func (ep *EventProcessor) processConfirmedEvents(ctx context.Context) error {
	events, err := ep.chainStore.GetConfirmedEvents(eventProcessBatchSize)
	if err != nil {
		return fmt.Errorf("failed to get confirmed events: %w", err)
	}

	for _, event := range events {
		handler, ok := ep.handlers[event.Type]
		if !ok {
			continue
		}

		if err := handler.HandleEvent(ctx, &event); err != nil {
			ep.logger.Error().
				Err(err).
				Str("event_id", event.EventID).
				Str("type", event.Type).
				Msg("failed to process event")
		}
	}

	return nil
}

// markEventCompleted atomically records the vote hash and flips CONFIRMED -> COMPLETED.
func markEventCompleted(chainStore *ChainStore, logger zerolog.Logger, event *store.Event, voteTxHash string) error {
	rowsAffected, err := chainStore.UpdateStatusAndVoteTxHash(event.EventID, store.StatusConfirmed, store.StatusCompleted, voteTxHash)
	if err != nil {
		return fmt.Errorf("failed to update event status after successful vote: %w", err)
	}

	if rowsAffected == 0 {
		return nil // already completed
	}

	logger.Info().
		Str("event_id", event.EventID).
		Str("type", event.Type).
		Str("vote_tx_hash", voteTxHash).
		Msg("event marked as COMPLETED")

	return nil
}

// eventTxHash extracts the tx hash from an EventID (format: "txHash:logIndex"
// or "signature:logIndex"), converting base58 signatures to 0x-prefixed hex.
// Falls back to the raw value if conversion fails.
func eventTxHash(eventID string) string {
	txHash := ""
	parts := strings.Split(eventID, ":")
	if len(parts) > 0 {
		txHash = parts[0]
	}

	txHashHex, err := base58ToHex(txHash)
	if err != nil {
		return txHash
	}
	return txHashHex
}

// base58ToHex converts a base58 encoded string to hex format (0x...)
func base58ToHex(base58Str string) (string, error) {
	if base58Str == "" {
		return "0x", nil
	}

	// Check if it's already in hex format
	if strings.HasPrefix(base58Str, "0x") {
		return base58Str, nil
	}

	// Decode base58 to bytes
	decoded, err := base58.Decode(base58Str)
	if err != nil {
		return "", fmt.Errorf("failed to decode base58: %w", err)
	}

	// Convert to hex with 0x prefix
	return "0x" + hex.EncodeToString(decoded), nil
}
