// Package push provides a client for listening to Push Chain events.
// It handles event polling, parsing, and persistence with proper error handling,
// graceful shutdown, and concurrent safety.
package push

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pushchain/push-chain-node/universalClient/pushcore"
	"github.com/pushchain/push-chain-node/universalClient/store"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

// Default configuration values.
const (
	DefaultPollInterval = 5 * time.Second
	DefaultChunkSize    = 1000
	DefaultQueryLimit   = 100

	minPollInterval = 1 * time.Second
	maxPollInterval = 5 * time.Minute
)

// Sentinel errors for the Push listener.
var (
	ErrAlreadyRunning  = errors.New("push listener is already running")
	ErrNotRunning      = errors.New("push listener is not running")
	ErrNilClient       = errors.New("push client cannot be nil")
	ErrNilDatabase     = errors.New("database connection cannot be nil")
	ErrInvalidInterval = errors.New("poll interval out of valid range")
)

// Config holds configuration for the Push listener.
type Config struct {
	// PollInterval is the duration between polling cycles.
	// Must be between 1 second and 5 minutes.
	PollInterval time.Duration

	// ChunkSize is the number of blocks to process in each batch.
	// Defaults to 1000 if not specified.
	ChunkSize uint64

	// QueryLimit is the maximum number of transactions to fetch per query.
	// Defaults to 100 if not specified.
	QueryLimit uint64
}

// Validate validates the configuration and applies defaults where necessary.
func (c *Config) Validate() error {
	if c.PollInterval < minPollInterval || c.PollInterval > maxPollInterval {
		return fmt.Errorf("%w: must be between %v and %v, got %v",
			ErrInvalidInterval, minPollInterval, maxPollInterval, c.PollInterval)
	}
	return nil
}

// applyDefaults sets default values for zero-value fields.
func (c *Config) applyDefaults() {
	if c.PollInterval == 0 {
		c.PollInterval = DefaultPollInterval
	}
	if c.ChunkSize == 0 {
		c.ChunkSize = DefaultChunkSize
	}
	if c.QueryLimit == 0 {
		c.QueryLimit = DefaultQueryLimit
	}
}

// PushClient defines the interface for interacting with the Push chain.
// This allows for easier testing and dependency injection.
type PushClient interface {
	GetLatestBlock() (uint64, error)
	GetTxsByEvents(query string, minHeight, maxHeight uint64, limit uint64) ([]*pushcore.TxResult, error)
}

// Listener listens for events from the Push chain and stores them in the database.
type Listener struct {
	logger     zerolog.Logger
	pushClient PushClient
	db         *gorm.DB
	cfg        Config

	watcher *EventWatcher
	mu      sync.RWMutex
	running atomic.Bool
	stopCh  chan struct{}
}

// NewListener creates a new Push event listener.
// Returns an error if required dependencies are nil or configuration is invalid.
func NewListener(
	client PushClient,
	db *gorm.DB,
	logger zerolog.Logger,
	cfg *Config,
) (*Listener, error) {
	if client == nil {
		return nil, ErrNilClient
	}
	if db == nil {
		return nil, ErrNilDatabase
	}

	// Use default config if nil
	if cfg == nil {
		cfg = &Config{}
	}

	// Apply defaults first
	cfg.applyDefaults()

	// Then validate
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return &Listener{
		logger:     logger.With().Str("component", "push_listener").Logger(),
		pushClient: client,
		db:         db,
		cfg:        *cfg,
		stopCh:     make(chan struct{}),
	}, nil
}

// Start begins listening for events from the Push chain.
// Returns ErrAlreadyRunning if the listener is already running.
func (l *Listener) Start(ctx context.Context) error {
	if !l.running.CompareAndSwap(false, true) {
		return ErrAlreadyRunning
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	// Load last processed block from chain_states
	startBlock, err := l.getLastProcessedBlock()
	if err != nil {
		l.running.Store(false)
		return fmt.Errorf("failed to get last processed block: %w", err)
	}

	l.logger.Info().
		Uint64("start_block", startBlock).
		Dur("poll_interval", l.cfg.PollInterval).
		Uint64("chunk_size", l.cfg.ChunkSize).
		Msg("starting Push event listener")

	// Reset stop channel for new run
	l.stopCh = make(chan struct{})

	// Create and start event watcher
	l.watcher = NewEventWatcher(
		l.pushClient,
		l.db,
		l.logger,
		l.cfg,
		startBlock,
	)

	if err := l.watcher.Start(ctx); err != nil {
		l.running.Store(false)
		return fmt.Errorf("failed to start event watcher: %w", err)
	}

	l.logger.Info().Msg("Push event listener started successfully")
	return nil
}

// Stop gracefully stops the listener.
// Returns ErrNotRunning if the listener is not running.
func (l *Listener) Stop() error {
	if !l.running.CompareAndSwap(true, false) {
		return ErrNotRunning
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	l.logger.Info().Msg("stopping Push event listener")

	// Signal stop
	close(l.stopCh)

	// Stop the watcher
	if l.watcher != nil {
		l.watcher.Stop()
		l.watcher = nil
	}

	l.logger.Info().Msg("Push event listener stopped successfully")
	return nil
}

// IsRunning returns whether the listener is currently running.
func (l *Listener) IsRunning() bool {
	return l.running.Load()
}

// getLastProcessedBlock reads the last processed block from chain_states.
func (l *Listener) getLastProcessedBlock() (uint64, error) {
	var chainState store.ChainState
	result := l.db.First(&chainState)

	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			l.logger.Info().Msg("no previous state found, starting from block 0")
			return 0, nil
		}
		return 0, fmt.Errorf("failed to query chain state: %w", result.Error)
	}

	l.logger.Info().
		Uint64("block", chainState.LastBlock).
		Msg("resuming from last processed block")

	return chainState.LastBlock, nil
}
