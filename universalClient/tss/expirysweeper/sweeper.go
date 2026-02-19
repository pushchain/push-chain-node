package expirysweeper

import (
	"context"
	"encoding/json"
	"time"

	"github.com/pkg/errors"
	"github.com/rs/zerolog"

	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"

	"github.com/pushchain/push-chain-node/universalClient/pushcore"
	"github.com/pushchain/push-chain-node/universalClient/pushsigner"
	"github.com/pushchain/push-chain-node/universalClient/store"
	"github.com/pushchain/push-chain-node/universalClient/tss/eventstore"
)

const (
	defaultCheckInterval = 30 * time.Second
	sweepBatchSize       = 100

	statusSign = "SIGN"
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
		if event.Type == statusSign {
			if err := s.voteFailureAndMarkReverted(ctx, &event, "event expired before TSS could start"); err != nil {
				s.logger.Error().Err(err).Str("event_id", event.EventID).Msg("failed to sweep expired SIGN event")
				continue
			}
		} else {
			if err := s.eventStore.Update(event.EventID, map[string]any{"status": eventstore.StatusReverted}); err != nil {
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

// voteFailureAndMarkReverted submits a failure vote to Push chain and marks the event REVERTED.
func (s *Sweeper) voteFailureAndMarkReverted(ctx context.Context, event *store.Event, errorMsg string) error {
	var data uexecutortypes.OutboundCreatedEvent
	if err := json.Unmarshal(event.EventData, &data); err != nil {
		return errors.Wrapf(err, "failed to parse outbound event data for event %s", event.EventID)
	}

	if s.pushSigner == nil {
		s.logger.Warn().Str("event_id", event.EventID).Msg("pushSigner not configured, skipping failure vote")
	} else {
		observation := &uexecutortypes.OutboundObservation{
			Success:  false,
			TxHash:   "",
			ErrorMsg: errorMsg,
		}
		if _, err := s.pushSigner.VoteOutbound(ctx, data.TxID, data.UniversalTxId, observation); err != nil {
			return errors.Wrapf(err, "failed to vote failure for event %s", event.EventID)
		}
	}

	if err := s.eventStore.Update(event.EventID, map[string]any{"status": eventstore.StatusReverted}); err != nil {
		return errors.Wrapf(err, "failed to mark event %s as reverted", event.EventID)
	}
	s.logger.Info().
		Str("event_id", event.EventID).
		Str("tx_id", data.TxID).
		Str("error_msg", errorMsg).
		Msg("voted failure and marked REVERTED")
	return nil
}
