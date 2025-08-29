package core

import (
	"context"
	"time"

	"github.com/rs/zerolog"

	"github.com/rollchains/pchain/universalClient/config"
	"github.com/rollchains/pchain/universalClient/db"
)

// TransactionCleaner handles periodic cleanup of old confirmed transactions
type TransactionCleaner struct {
	db               *db.DB
	config           *config.Config
	ticker           *time.Ticker
	logger           zerolog.Logger
	stopCh           chan struct{}
	cleanupInterval  time.Duration
	retentionPeriod  time.Duration
}

// NewTransactionCleaner creates a new transaction cleaner
func NewTransactionCleaner(
	database *db.DB,
	cfg *config.Config,
	logger zerolog.Logger,
) *TransactionCleaner {
	return &TransactionCleaner{
		db:               database,
		config:           cfg,
		cleanupInterval:  cfg.TransactionCleanupInterval,
		retentionPeriod:  cfg.TransactionRetentionPeriod,
		logger:           logger.With().Str("component", "transaction_cleaner").Logger(),
		stopCh:           make(chan struct{}),
	}
}

// Start begins the periodic cleanup process
func (tc *TransactionCleaner) Start(ctx context.Context) error {
	tc.logger.Info().
		Dur("cleanup_interval", tc.cleanupInterval).
		Dur("retention_period", tc.retentionPeriod).
		Msg("starting transaction cleaner")

	// Perform initial cleanup
	if err := tc.performCleanup(); err != nil {
		tc.logger.Error().Err(err).Msg("failed to perform initial cleanup")
		// Don't fail startup on cleanup error, just log it
	}

	// Start periodic cleanup
	tc.ticker = time.NewTicker(tc.cleanupInterval)

	go func() {
		defer tc.ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				tc.logger.Info().Msg("context cancelled, stopping transaction cleaner")
				return
			case <-tc.stopCh:
				tc.logger.Info().Msg("stop signal received, stopping transaction cleaner")
				return
			case <-tc.ticker.C:
				if err := tc.performCleanup(); err != nil {
					tc.logger.Error().Err(err).Msg("failed to perform scheduled cleanup")
				}
			}
		}
	}()

	return nil
}

// Stop gracefully stops the transaction cleaner
func (tc *TransactionCleaner) Stop() {
	tc.logger.Info().Msg("stopping transaction cleaner")
	close(tc.stopCh)
	if tc.ticker != nil {
		tc.ticker.Stop()
	}
}

// performCleanup executes the actual cleanup of old confirmed transactions
func (tc *TransactionCleaner) performCleanup() error {
	start := time.Now()
	
	tc.logger.Debug().
		Dur("retention_period", tc.retentionPeriod).
		Msg("performing transaction cleanup")

	deletedCount, err := tc.db.DeleteOldConfirmedTransactions(tc.retentionPeriod)
	if err != nil {
		return err
	}

	// If we deleted transactions, checkpoint the WAL to prevent it from growing indefinitely
	if deletedCount > 0 {
		tc.checkpointWAL()
	}

	duration := time.Since(start)
	
	if deletedCount > 0 {
		tc.logger.Info().
			Int64("deleted_count", deletedCount).
			Dur("duration", duration).
			Msg("transaction cleanup completed")
	} else {
		tc.logger.Debug().
			Dur("duration", duration).
			Msg("transaction cleanup completed - no transactions to delete")
	}

	return nil
}

// checkpointWAL performs WAL checkpointing to prevent WAL file growth
func (tc *TransactionCleaner) checkpointWAL() {
	tc.logger.Debug().Msg("performing WAL checkpoint")
	
	// Use PRAGMA wal_checkpoint(TRUNCATE) to force a checkpoint and truncate the WAL
	if err := tc.db.Client().Exec("PRAGMA wal_checkpoint(TRUNCATE)").Error; err != nil {
		tc.logger.Warn().Err(err).Msg("failed to checkpoint WAL")
	} else {
		tc.logger.Debug().Msg("WAL checkpoint completed")
	}
}