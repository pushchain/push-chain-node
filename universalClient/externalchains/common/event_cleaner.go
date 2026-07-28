package common

import (
	"context"
	"fmt"
	"time"

	"github.com/pushchain/push-chain-node/universalClient/db"
	"github.com/rs/zerolog"
)

// Defaults applied when the per-chain config leaves cleanup settings unset.
// Chosen to keep the DB bounded even without explicit operator config:
// cleanup runs hourly, terminal events linger for a day before being purged.
const (
	defaultCleanupInterval = 1 * time.Hour
	defaultRetentionPeriod = 24 * time.Hour
)

// EventCleaner handles periodic cleanup of old confirmed events for a chain
type EventCleaner struct {
	database        *db.DB
	cleanupInterval time.Duration
	retentionPeriod time.Duration
	logger          zerolog.Logger
	ticker          *time.Ticker
	stopCh          chan struct{}
	running         bool
}

// NewEventCleaner creates a new event cleaner for a chain
func NewEventCleaner(
	database *db.DB,
	cleanupIntervalSeconds *int,
	retentionPeriodSeconds *int,
	chainID string,
	logger zerolog.Logger,
) *EventCleaner {
	cleanupInterval := defaultCleanupInterval
	if cleanupIntervalSeconds != nil {
		cleanupInterval = time.Duration(*cleanupIntervalSeconds) * time.Second
	}
	retentionPeriod := defaultRetentionPeriod
	if retentionPeriodSeconds != nil {
		retentionPeriod = time.Duration(*retentionPeriodSeconds) * time.Second
	}
	return &EventCleaner{
		database:        database,
		cleanupInterval: cleanupInterval,
		retentionPeriod: retentionPeriod,
		logger:          logger.With().Str("component", "event_cleaner").Str("chain", chainID).Logger(),
	}
}

// Start begins the periodic cleanup process
func (ec *EventCleaner) Start(ctx context.Context) error {
	if ec.running {
		return fmt.Errorf("event cleaner is already running")
	}

	ec.logger.Debug().
		Str("cleanup_interval", ec.cleanupInterval.String()).
		Str("retention_period", ec.retentionPeriod.String()).
		Msg("starting event cleaner")

	// Perform initial cleanup
	if err := ec.performCleanup(); err != nil {
		ec.logger.Error().Err(err).Msg("failed to perform initial cleanup")
		// Don't fail startup on cleanup error, just log it
	}

	ec.running = true
	ec.stopCh = make(chan struct{})
	ec.ticker = time.NewTicker(ec.cleanupInterval)

	go func() {
		defer ec.ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				ec.logger.Debug().Msg("context cancelled, stopping event cleaner")
				return
			case <-ec.stopCh:
				ec.logger.Debug().Msg("stop signal received, stopping event cleaner")
				return
			case <-ec.ticker.C:
				if err := ec.performCleanup(); err != nil {
					ec.logger.Error().Err(err).Msg("failed to perform scheduled cleanup")
				}
			}
		}
	}()

	return nil
}

// Stop gracefully stops the event cleaner. No-op if not running.
func (ec *EventCleaner) Stop() {
	if !ec.running {
		return
	}
	ec.logger.Debug().Msg("stopping event cleaner")
	if ec.ticker != nil {
		ec.ticker.Stop()
	}
	close(ec.stopCh)
	ec.running = false
}

// performCleanup executes cleanup of terminal events (COMPLETED, REORGED, REVERTED)
func (ec *EventCleaner) performCleanup() error {
	start := time.Now()

	ec.logger.Debug().
		Str("retention_period", ec.retentionPeriod.String()).
		Msg("performing event cleanup")

	cutoffTime := time.Now().Add(-ec.retentionPeriod)

	chainStore := NewChainStore(ec.database)
	deletedCount, err := chainStore.DeleteTerminalEvents(cutoffTime)
	if err != nil {
		return fmt.Errorf("failed to cleanup events: %w", err)
	}

	duration := time.Since(start)

	if deletedCount > 0 {
		ec.logger.Info().
			Int64("deleted_count", deletedCount).
			Str("duration", duration.String()).
			Msg("terminal event cleanup completed (COMPLETED, REORGED, REVERTED)")

		// Checkpoint WAL after cleanup
		ec.checkpointWAL()
	} else {
		ec.logger.Debug().
			Str("duration", duration.String()).
			Msg("event cleanup completed - no terminal events to delete")
	}

	return nil
}

// checkpointWAL performs WAL checkpointing for the database
func (ec *EventCleaner) checkpointWAL() {
	ec.logger.Debug().Msg("performing WAL checkpoint")

	// Use PRAGMA wal_checkpoint(TRUNCATE) to force a checkpoint and truncate the WAL
	if err := ec.database.Client().Exec("PRAGMA wal_checkpoint(TRUNCATE)").Error; err != nil {
		ec.logger.Warn().
			Err(err).
			Msg("failed to checkpoint WAL")
	} else {
		ec.logger.Debug().Msg("WAL checkpoint completed")
	}
}
