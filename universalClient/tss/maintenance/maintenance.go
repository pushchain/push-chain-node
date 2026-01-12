// Package maintenance handles TSS event maintenance tasks including expiry processing and database cleanup.
package maintenance

import (
	"context"
	"sync"
	"time"

	"github.com/pkg/errors"
	"github.com/rs/zerolog"

	"github.com/pushchain/push-chain-node/universalClient/pushcore"
	"github.com/pushchain/push-chain-node/universalClient/store"
	"github.com/pushchain/push-chain-node/universalClient/tss/eventstore"
)

// OutboundVoter handles voting for outbound transaction results.
type OutboundVoter interface {
	// VoteOutbound votes on an outbound transaction observation.
	// isSuccess indicates whether the transaction succeeded.
	// For success: txHash and blockHeight must be provided (blockHeight > 0).
	// For revert: reason must be provided; txHash and blockHeight are optional (if txHash is provided, blockHeight must be > 0).
	VoteOutbound(ctx context.Context, txID string, isSuccess bool, txHash string, blockHeight uint64, reason string) (string, error)
}

// Config contains configuration for the maintenance handler.
type Config struct {
	// PollInterval is how often to check for expired events (default: 30s)
	PollInterval time.Duration

	// CleanupInterval is how often to clean up terminal events (default: 1h)
	CleanupInterval time.Duration
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		PollInterval:    30 * time.Second,
		CleanupInterval: 1 * time.Hour,
	}
}

// TODO: Handle BROADCASTED events completion via chain event listeners.
// Instead of polling for transaction confirmations, chain event listeners should:
// 1. Listen to gateway contract events on each chain
// 2. When a gateway event is received with enough confirmations for that chain,
//    mark the corresponding BROADCASTED event as COMPLETED
// 3. Vote for outbound success/revert based on the gateway event result
// This will be implemented in the chain-specific event listeners.

// Handler handles TSS event maintenance tasks including expiry processing and database cleanup.
type Handler struct {
	eventStore *eventstore.Store
	pushCore   *pushcore.Client
	voter      OutboundVoter
	config     Config
	logger     zerolog.Logger

	mu      sync.RWMutex
	running bool
	stopCh  chan struct{}
}

// NewHandler creates a new maintenance handler.
func NewHandler(
	eventStore *eventstore.Store,
	pushCore *pushcore.Client,
	voter OutboundVoter,
	config Config,
	logger zerolog.Logger,
) *Handler {
	if config.PollInterval == 0 || config.CleanupInterval == 0 {
		defaultConfig := DefaultConfig()
		if config.PollInterval == 0 {
			config.PollInterval = defaultConfig.PollInterval
		}
		if config.CleanupInterval == 0 {
			config.CleanupInterval = defaultConfig.CleanupInterval
		}
	}
	return &Handler{
		eventStore: eventStore,
		pushCore:   pushCore,
		voter:      voter,
		config:     config,
		logger:     logger.With().Str("component", "tss_maintenance").Logger(),
		stopCh:     make(chan struct{}),
	}
}

// Start begins the maintenance handler.
func (h *Handler) Start(ctx context.Context) error {
	h.mu.Lock()
	if h.running {
		h.mu.Unlock()
		return errors.New("maintenance handler already running")
	}
	h.running = true
	h.mu.Unlock()

	h.logger.Info().
		Dur("poll_interval", h.config.PollInterval).
		Dur("cleanup_interval", h.config.CleanupInterval).
		Msg("starting TSS maintenance handler")

	go h.runLoop(ctx)
	return nil
}

// Stop stops the maintenance handler.
func (h *Handler) Stop() {
	h.mu.Lock()
	defer h.mu.Unlock()

	if !h.running {
		return
	}

	close(h.stopCh)
	h.running = false
	h.logger.Info().Msg("TSS maintenance handler stopped")
}

func (h *Handler) runLoop(ctx context.Context) {
	expiryTicker := time.NewTicker(h.config.PollInterval)
	defer expiryTicker.Stop()

	cleanupTicker := time.NewTicker(h.config.CleanupInterval)
	defer cleanupTicker.Stop()

	// Run immediately on start
	h.checkExpired(ctx)
	h.clearTerminalEvents(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-h.stopCh:
			return
		case <-expiryTicker.C:
			h.checkExpired(ctx)
		case <-cleanupTicker.C:
			h.clearTerminalEvents(ctx)
		}
	}
}

func (h *Handler) checkExpired(ctx context.Context) {
	// Handle expired events
	if err := h.handleExpiredEvents(ctx); err != nil {
		h.logger.Error().Err(err).Msg("error handling expired events")
	}
}

// clearTerminalEvents clears expired, reverted, and completed events from the database.
func (h *Handler) clearTerminalEvents(ctx context.Context) {
	deletedCount, err := h.eventStore.ClearTerminalEvents()
	if err != nil {
		h.logger.Error().Err(err).Msg("error clearing terminal events")
		return
	}

	if deletedCount > 0 {
		h.logger.Info().
			Int64("deleted_count", deletedCount).
			Msg("cleared terminal events (expired, reverted, completed) from database")
	}
}

// handleExpiredEvents finds and processes expired events.
func (h *Handler) handleExpiredEvents(ctx context.Context) error {
	currentBlock, err := h.pushCore.GetLatestBlockNum()
	if err != nil {
		return errors.Wrap(err, "failed to get current block")
	}

	// Get all expired events (PENDING, IN_PROGRESS, or BROADCASTED)
	events, err := h.eventStore.GetExpiredEvents(currentBlock)
	if err != nil {
		return errors.Wrap(err, "failed to get expired events")
	}

	if len(events) == 0 {
		return nil
	}

	h.logger.Info().Int("count", len(events)).Msg("processing expired events")

	for _, event := range events {
		if err := h.processExpiredEvent(ctx, &event); err != nil {
			h.logger.Error().
				Err(err).
				Str("event_id", event.EventID).
				Str("type", event.Type).
				Str("status", event.Status).
				Msg("failed to process expired event")
		}
	}

	return nil
}

func (h *Handler) processExpiredEvent(ctx context.Context, event *store.PCEvent) error {
	h.logger.Info().
		Str("event_id", event.EventID).
		Str("type", event.Type).
		Str("status", event.Status).
		Uint64("expiry_block", event.ExpiryBlockHeight).
		Msg("processing expired event")

	switch event.Type {
	case "KEYGEN", "KEYREFRESH", "QUORUM_CHANGE":
		// For key events, mark as EXPIRED
		if err := h.eventStore.UpdateStatus(event.EventID, eventstore.StatusExpired, "expired"); err != nil {
			return errors.Wrap(err, "failed to mark key event as expired")
		}
		h.logger.Info().
			Str("event_id", event.EventID).
			Str("status", event.Status).
			Msg("key event marked as expired")

	case "SIGN":
		// For sign events, vote for revert on Push chain and mark as REVERTED
		// For outbound events, txID is the eventID
		txID := event.EventID

		// Determine reason based on current status
		var reason string
		var txHash string
		var blockHeight uint64

		switch event.Status {
		case eventstore.StatusPending:
			reason = "expired before signing completed"
			// No txHash or blockHeight for pending events
		case eventstore.StatusInProgress:
			reason = "expired during TSS signing"
			// No txHash or blockHeight for in-progress events
		case eventstore.StatusBroadcasted:
			reason = "expired after broadcast, no confirmations received"
			// If broadcasted, we might have a txHash
			if event.TxHash != "" {
				// Parse CAIP format to get raw hash (chain expects simple hash, not CAIP)
				var err error
				_, txHash, err = parseCaipTxHash(event.TxHash)
				if err != nil {
					h.logger.Warn().Err(err).Str("event_id", event.EventID).Msg("failed to parse txHash, voting without it")
					txHash = ""
				}
			}
		default:
			reason = "expired"
		}

		if h.voter != nil {
			voteTxHash, err := h.voter.VoteOutbound(ctx, txID, false, txHash, blockHeight, reason)
			if err != nil {
				h.logger.Error().Err(err).Str("event_id", event.EventID).Msg("failed to vote for revert")
				// Still mark as reverted locally
			} else {
				h.logger.Info().
					Str("event_id", event.EventID).
					Str("vote_tx_hash", voteTxHash).
					Str("original_status", event.Status).
					Msg("voted for outbound revert (expired)")
			}
		}

		if err := h.eventStore.UpdateStatus(event.EventID, eventstore.StatusReverted, reason); err != nil {
			return errors.Wrap(err, "failed to mark sign event as reverted")
		}
		h.logger.Info().
			Str("event_id", event.EventID).
			Str("original_status", event.Status).
			Msg("sign event marked as reverted (expired)")

	default:
		h.logger.Warn().Str("event_id", event.EventID).Str("type", event.Type).Msg("unknown event type for expiry handling")
	}

	return nil
}

// parseCaipTxHash parses a CAIP format tx hash: {chainId}:{txHash}
func parseCaipTxHash(caipTxHash string) (chainID, txHash string, err error) {
	// Find the last colon (chainID can contain colons, e.g., "eip155:11155111")
	lastColon := -1
	for i := len(caipTxHash) - 1; i >= 0; i-- {
		if caipTxHash[i] == ':' {
			lastColon = i
			break
		}
	}

	if lastColon == -1 || lastColon == 0 || lastColon == len(caipTxHash)-1 {
		return "", "", errors.Errorf("invalid CAIP tx hash format: %s", caipTxHash)
	}

	return caipTxHash[:lastColon], caipTxHash[lastColon+1:], nil
}
