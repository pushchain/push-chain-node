package expirysweeper

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/rs/zerolog"

	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
	utsstypes "github.com/pushchain/push-chain-node/x/utss/types"

	"github.com/pushchain/push-chain-node/universalClient/pushcore"
	"github.com/pushchain/push-chain-node/universalClient/pushsigner"
	"github.com/pushchain/push-chain-node/universalClient/store"
	"github.com/pushchain/push-chain-node/universalClient/tss/eventstore"
)

const (
	defaultCheckInterval = 30 * time.Second
	sweepBatchSize       = 100
)

// Config holds configuration for the expiry sweeper.
type Config struct {
	EventStore    *eventstore.Store
	PushCore      *pushcore.Client
	PushSigner    *pushsigner.Signer // Optional — nil disables failure voting
	CheckInterval time.Duration
	Logger        zerolog.Logger
}

// Sweeper polls for CONFIRMED events past their expiry block and marks them REVERTED.
// For SIGN events a failure vote is submitted to Push chain first so the protocol
// can refund the user. Key events (KEYGEN/KEYREFRESH/QUORUM_CHANGE) are marked
// REVERTED directly — TSS never started so there is no outbound to vote on.
type Sweeper struct {
	eventStore    *eventstore.Store
	pushCore      *pushcore.Client
	pushSigner    *pushsigner.Signer
	checkInterval time.Duration
	logger        zerolog.Logger
}

// NewSweeper creates a new expiry sweeper.
func NewSweeper(cfg Config) *Sweeper {
	interval := cfg.CheckInterval
	if interval == 0 {
		interval = defaultCheckInterval
	}
	return &Sweeper{
		eventStore:    cfg.EventStore,
		pushCore:      cfg.PushCore,
		pushSigner:    cfg.PushSigner,
		checkInterval: interval,
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
	currentBlock, err := s.pushCore.GetLatestBlock(ctx)
	if err != nil {
		s.logger.Warn().Err(err).Msg("failed to get current block, skipping sweep")
		return
	}

	events, err := s.eventStore.GetExpiredConfirmedEvents(currentBlock, sweepBatchSize)
	if err != nil {
		s.logger.Error().Err(err).Msg("failed to query expired confirmed events")
		return
	}
	if len(events) == 0 {
		return
	}

	swept := 0
	for _, event := range events {
		if event.Type == store.EventTypeSignOutbound {
			if err := s.voteOutboundFailureAndMarkReverted(ctx, &event, "event expired before TSS could start"); err != nil {
				s.logger.Error().Err(err).Str("event_id", event.EventID).Msg("failed to sweep expired SIGN_OUTBOUND event")
				continue
			}
		} else if event.Type == store.EventTypeSignFundMigrate {
			if err := s.voteFundMigrationFailureAndMarkReverted(ctx, &event, "event expired before TSS could start"); err != nil {
				s.logger.Error().Err(err).Str("event_id", event.EventID).Msg("failed to sweep expired SIGN_FUND_MIGRATE event")
				continue
			}
		} else {
			if err := s.eventStore.Update(event.EventID, map[string]any{"status": store.StatusReverted}); err != nil {
				s.logger.Error().Err(err).Str("event_id", event.EventID).Msg("failed to revert expired key event")
				continue
			}
		}
		swept++
	}

	s.logger.Info().
		Int("swept", swept).
		Int("total_expired", len(events)).
		Uint64("current_block", currentBlock).
		Msg("swept expired confirmed events")
}

// voteOutboundFailureAndMarkReverted submits a failure vote for an outbound event and marks it REVERTED.
func (s *Sweeper) voteOutboundFailureAndMarkReverted(ctx context.Context, event *store.Event, errorMsg string) error {
	var data uexecutortypes.OutboundCreatedEvent
	if err := json.Unmarshal(event.EventData, &data); err != nil {
		return fmt.Errorf("failed to parse outbound event data for event %s: %w", event.EventID, err)
	}

	fields := map[string]any{"status": store.StatusReverted}

	if s.pushSigner == nil {
		s.logger.Warn().Str("event_id", event.EventID).Msg("pushSigner not configured, skipping failure vote")
	} else {
		observation := &uexecutortypes.OutboundObservation{
			Success:    false,
			TxHash:     "",
			ErrorMsg:   errorMsg,
			GasFeeUsed: "0",
		}
		voteTxHash, err := s.pushSigner.VoteOutbound(ctx, data.TxID, data.UniversalTxId, observation)
		if err != nil {
			return fmt.Errorf("failed to vote failure for event %s: %w", event.EventID, err)
		}
		fields["vote_tx_hash"] = voteTxHash
	}

	if err := s.eventStore.Update(event.EventID, fields); err != nil {
		return fmt.Errorf("failed to mark event %s as reverted: %w", event.EventID, err)
	}
	s.logger.Info().
		Str("event_id", event.EventID).
		Str("tx_id", data.TxID).
		Str("error_msg", errorMsg).
		Msg("voted outbound failure and marked REVERTED")
	return nil
}

// voteFundMigrationFailureAndMarkReverted submits a failure vote for a fund migration event and marks it REVERTED.
func (s *Sweeper) voteFundMigrationFailureAndMarkReverted(ctx context.Context, event *store.Event, errorMsg string) error {
	var data utsstypes.FundMigrationInitiatedEventData
	if err := json.Unmarshal(event.EventData, &data); err != nil {
		return fmt.Errorf("failed to parse fund migration event data for event %s: %w", event.EventID, err)
	}

	fields := map[string]any{"status": store.StatusReverted}

	if s.pushSigner == nil {
		s.logger.Warn().Str("event_id", event.EventID).Msg("pushSigner not configured, skipping failure vote")
	} else {
		voteTxHash, err := s.pushSigner.VoteFundMigration(ctx, data.MigrationID, "", false)
		if err != nil {
			return fmt.Errorf("failed to vote fund migration failure for event %s: %w", event.EventID, err)
		}
		fields["vote_tx_hash"] = voteTxHash
	}

	if err := s.eventStore.Update(event.EventID, fields); err != nil {
		return fmt.Errorf("failed to mark event %s as reverted: %w", event.EventID, err)
	}
	s.logger.Info().
		Str("event_id", event.EventID).
		Uint64("migration_id", data.MigrationID).
		Str("error_msg", errorMsg).
		Msg("voted fund migration failure and marked REVERTED")
	return nil
}
