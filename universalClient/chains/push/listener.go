package push

import (
	"context"
	"sync"
	"time"

	"github.com/pushchain/push-chain-node/universalClient/pushcore"
	"github.com/pushchain/push-chain-node/universalClient/tss/eventstore"
	"github.com/rs/zerolog"
)

// PushTSSEventListener listens for TSS events from the Push chain
// and stores them in the database for processing.
type PushTSSEventListener struct {
	logger  zerolog.Logger
	watcher *EventWatcher

	mu      sync.RWMutex
	running bool
	healthy bool
}

// NewPushTSSEventListener creates a new Push TSS event listener.
func NewPushTSSEventListener(
	client *pushcore.Client,
	store *eventstore.Store,
	logger zerolog.Logger,
) *PushTSSEventListener {
	return &PushTSSEventListener{
		logger:  logger.With().Str("component", "push_tss_listener").Logger(),
		watcher: NewEventWatcher(client, store, logger),
		running: false,
		healthy: false,
	}
}

// Config holds configuration for the listener.
type Config struct {
	PollInterval time.Duration
	StartBlock   uint64
}

// DefaultConfig returns the default listener configuration.
func DefaultConfig() Config {
	return Config{
		PollInterval: DefaultPollInterval,
		StartBlock:   0, // Start from the beginning (or recent blocks)
	}
}

// WithConfig applies configuration to the listener.
func (l *PushTSSEventListener) WithConfig(cfg Config) *PushTSSEventListener {
	if cfg.PollInterval > 0 {
		l.watcher.SetPollInterval(cfg.PollInterval)
	}
	if cfg.StartBlock > 0 {
		l.watcher.SetLastBlock(cfg.StartBlock)
	}
	return l
}

// Start begins listening for TSS events from the Push chain.
func (l *PushTSSEventListener) Start(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.running {
		return nil // Already running
	}

	l.logger.Info().Msg("starting Push TSS event listener")

	// Start the event watcher
	l.watcher.Start(ctx)

	l.running = true
	l.healthy = true

	l.logger.Info().Msg("Push TSS event listener started")
	return nil
}

// Stop stops the listener.
func (l *PushTSSEventListener) Stop() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if !l.running {
		return nil // Already stopped
	}

	l.logger.Info().Msg("stopping Push TSS event listener")

	l.watcher.Stop()

	l.running = false
	l.healthy = false

	l.logger.Info().Msg("Push TSS event listener stopped")
	return nil
}

// IsHealthy returns whether the listener is operating normally.
func (l *PushTSSEventListener) IsHealthy() bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.healthy
}

// IsRunning returns whether the listener is currently running.
func (l *PushTSSEventListener) IsRunning() bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.running
}

// GetLastProcessedBlock returns the last block height that was processed.
func (l *PushTSSEventListener) GetLastProcessedBlock() uint64 {
	return l.watcher.GetLastBlock()
}

// SetHealthy sets the health status (useful for testing or external health checks).
func (l *PushTSSEventListener) SetHealthy(healthy bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.healthy = healthy
}
