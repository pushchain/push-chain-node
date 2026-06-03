package expirysweeper

import (
	"context"
	"time"

	"github.com/rs/zerolog"

	"github.com/pushchain/push-chain-node/universalClient/pushcore"
	"github.com/pushchain/push-chain-node/universalClient/tss/eventstore"
)

const (
	defaultCheckInterval = 30 * time.Second
	defaultMaxEventAge   = 1 * time.Hour
)

// Config holds configuration for the expiry sweeper.
type Config struct {
	EventStore    *eventstore.Store
	PushCore      *pushcore.Client
	CheckInterval time.Duration
	MaxEventAge   time.Duration // Events older than this are deleted (default: 1h).
	Logger        zerolog.Logger
}

// Sweeper periodically drops events that have expired or grown too old.
// Two deletion triggers:
//  1. Block-based: events past their ExpiryBlockHeight (KEY events have a
//     protocol-driven expiry; SIGN events have ExpiryBlockHeight=0 and skip).
//  2. Age-based: only UNSIGNED events (status CONFIRMED or IN_PROGRESS) older
//     than MaxEventAge. SIGNED and later statuses carry local commitments and
//     are preserved.
//
// Dropping is safe because push chain is the source of truth: if a dropped
// event is still pending on push chain, the push chain pending-tx parser
// re-populates it on its next poll. Anything truly stale (no longer pending
// upstream) stays dropped, which is the desired cleanup behaviour.
type Sweeper struct {
	eventStore    *eventstore.Store
	pushCore      *pushcore.Client
	checkInterval time.Duration
	maxEventAge   time.Duration
	logger        zerolog.Logger
}

// NewSweeper creates a new expiry sweeper.
func NewSweeper(cfg Config) *Sweeper {
	interval := cfg.CheckInterval
	if interval == 0 {
		interval = defaultCheckInterval
	}
	maxAge := cfg.MaxEventAge
	if maxAge == 0 {
		maxAge = defaultMaxEventAge
	}
	return &Sweeper{
		eventStore:    cfg.EventStore,
		pushCore:      cfg.PushCore,
		checkInterval: interval,
		maxEventAge:   maxAge,
		logger:        cfg.Logger.With().Str("component", "expiry_sweeper").Logger(),
	}
}

// Start begins the background sweep loop.
func (s *Sweeper) Start(ctx context.Context) {
	go s.run(ctx)
}

func (s *Sweeper) run(ctx context.Context) {
	ticker := time.NewTicker(s.checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.sweep(ctx)
		}
	}
}

func (s *Sweeper) sweep(ctx context.Context) {
	// Block-based deletion: events past their protocol-driven ExpiryBlockHeight.
	if currentBlock, err := s.pushCore.GetLatestBlock(ctx); err != nil {
		s.logger.Warn().Err(err).Msg("failed to get current block, skipping block-expiry sweep")
	} else if deleted, err := s.eventStore.DeleteExpiredEvents(currentBlock); err != nil {
		s.logger.Error().Err(err).Msg("failed to delete expired events")
	} else if deleted > 0 {
		s.logger.Info().Int64("deleted", deleted).Uint64("current_block", currentBlock).
			Msg("deleted block-expired events")
	}

	// Age-based deletion: only UNSIGNED events older than maxEventAge.
	// Push chain re-populates anything still pending upstream.
	cutoff := time.Now().Add(-s.maxEventAge)
	if deleted, err := s.eventStore.DeleteOldUnsignedEvents(cutoff); err != nil {
		s.logger.Error().Err(err).Msg("failed to delete old unsigned events")
	} else if deleted > 0 {
		s.logger.Info().Int64("deleted", deleted).Time("cutoff", cutoff).
			Msg("deleted age-expired unsigned events")
	}
}
