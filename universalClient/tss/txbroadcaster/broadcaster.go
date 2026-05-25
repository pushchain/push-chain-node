package txbroadcaster

import (
	"context"
	"encoding/json"
	"time"

	"github.com/rs/zerolog"

	"github.com/pushchain/push-chain-node/universalClient/chains"
	"github.com/pushchain/push-chain-node/universalClient/store"
	"github.com/pushchain/push-chain-node/universalClient/tss/eventstore"
	"github.com/pushchain/push-chain-node/universalClient/tss/txflow"
)

type Config struct {
	EventStore    *eventstore.Store
	Chains        *chains.Chains
	CheckInterval time.Duration
	Logger        zerolog.Logger
	GetTSSAddress func(ctx context.Context) (string, error)
}

type Broadcaster struct {
	eventStore    *eventstore.Store
	chains        *chains.Chains
	checkInterval time.Duration
	logger        zerolog.Logger
	getTSSAddress func(ctx context.Context) (string, error)

	// svmBroadcastAttempts is an in-memory failure counter per event_id used to
	// cap SVM retries before escalating to REVERT. Lost on process restart by
	// design — restart resets all counters, giving the operator a fresh budget.
	// Temporary mechanism; the signature-deadline system will supersede it.
	// Safe without a mutex: processSigned drains events serially.
	svmBroadcastAttempts map[string]uint32
}

func NewBroadcaster(cfg Config) *Broadcaster {
	interval := cfg.CheckInterval
	if interval == 0 {
		interval = 15 * time.Second
	}
	return &Broadcaster{
		eventStore:           cfg.EventStore,
		chains:               cfg.Chains,
		checkInterval:        interval,
		logger:               cfg.Logger.With().Str("component", "txbroadcaster").Logger(),
		getTSSAddress:        cfg.GetTSSAddress,
		svmBroadcastAttempts: make(map[string]uint32),
	}
}

// Start begins the background broadcast loop.
func (b *Broadcaster) Start(ctx context.Context) {
	go b.run(ctx)
}

func (b *Broadcaster) run(ctx context.Context) {
	ticker := time.NewTicker(b.checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			b.processSigned(ctx)
		}
	}
}

const processSignedBatchSize = 100

// processSigned drains all SIGNED events in batches.
func (b *Broadcaster) processSigned(ctx context.Context) {
	if b.chains == nil {
		return
	}
	for {
		events, err := b.eventStore.GetSignedSignEvents(processSignedBatchSize)
		if err != nil {
			b.logger.Warn().Err(err).Msg("failed to get signed events")
			return
		}
		if len(events) == 0 {
			return
		}
		for i := range events {
			b.broadcastEvent(ctx, &events[i])
		}
		if len(events) < processSignedBatchSize {
			return
		}
	}
}

// broadcastEvent dispatches to the appropriate handler based on event type.
func (b *Broadcaster) broadcastEvent(ctx context.Context, event *store.Event) {
	switch event.Type {
	case store.EventTypeSignOutbound:
		b.broadcastOutbound(ctx, event)
	case store.EventTypeSignFundMigrate:
		b.broadcastFundMigration(ctx, event)
	default:
		b.logger.Warn().Str("event_id", event.EventID).Str("type", event.Type).
			Msg("unknown signed event type, skipping")
	}
}

// ---------------------------------------------------------------------------
// Outbound broadcast (parsing + chain dispatch)
// ---------------------------------------------------------------------------

// broadcastOutbound parses outbound event data and delegates to chain-specific broadcast.
func (b *Broadcaster) broadcastOutbound(ctx context.Context, event *store.Event) {
	var data txflow.SignedOutboundData
	if err := json.Unmarshal(event.EventData, &data); err != nil {
		b.logger.Warn().Err(err).Str("event_id", event.EventID).Msg("failed to parse signed outbound data")
		return
	}
	if data.SigningData == nil {
		b.logger.Warn().Str("event_id", event.EventID).Msg("signing_data missing from outbound event")
		return
	}

	chainID := data.DestinationChain
	if !b.chains.IsChainOutboundEnabled(chainID) {
		b.logger.Warn().Str("chain", chainID).Str("event_id", event.EventID).
			Msg("outbound disabled, skipping broadcast")
		return
	}

	if b.chains.IsEVMChain(chainID) {
		b.broadcastOutboundEVM(ctx, event, &data, chainID)
	} else {
		b.broadcastOutboundSVM(ctx, event, &data, chainID)
	}
}

// ---------------------------------------------------------------------------
// Fund migration broadcast (parsing + chain dispatch)
// ---------------------------------------------------------------------------

// broadcastFundMigration parses fund migration event data and delegates to chain-specific broadcast.
func (b *Broadcaster) broadcastFundMigration(ctx context.Context, event *store.Event) {
	var data txflow.SignedFundMigrationData
	if err := json.Unmarshal(event.EventData, &data); err != nil {
		b.logger.Warn().Err(err).Str("event_id", event.EventID).Msg("failed to parse fund migration signed data")
		return
	}
	if data.SigningData == nil {
		b.logger.Warn().Str("event_id", event.EventID).Msg("signing_data missing from fund migration event")
		return
	}

	chainID := data.Chain

	if b.chains.IsEVMChain(chainID) {
		b.broadcastFundMigrationEVM(ctx, event, &data, chainID)
	} else {
		b.logger.Warn().Str("chain", chainID).Str("event_id", event.EventID).
			Msg("fund migration not supported for this chain type")
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// markBroadcasted updates the event status to BROADCASTED with the given tx hash.
func (b *Broadcaster) markBroadcasted(event *store.Event, chainID, txHash string) {
	caipTxHash := chainID + ":" + txHash
	if err := b.eventStore.Update(event.EventID, map[string]any{
		"broadcasted_tx_hash": caipTxHash,
		"status":              store.StatusBroadcasted,
	}); err != nil {
		b.logger.Warn().Err(err).Str("event_id", event.EventID).Msg("failed to update event to BROADCASTED")
		return
	}
	b.logger.Info().
		Str("event_id", event.EventID).
		Str("type", event.Type).
		Str("chain", chainID).
		Msg("event marked as BROADCASTED")
}
