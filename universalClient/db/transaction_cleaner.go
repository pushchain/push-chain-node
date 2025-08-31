package db

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog"

	"github.com/rollchains/pchain/universalClient/config"
)

// TransactionCleaner handles periodic cleanup of old confirmed transactions across all chain databases
type TransactionCleaner struct {
	dbManager        *ChainDBManager
	config           *config.Config
	ticker           *time.Ticker
	logger           zerolog.Logger
	stopCh           chan struct{}
	cleanupInterval  time.Duration
	retentionPeriod  time.Duration
}

// NewTransactionCleaner creates a new transaction cleaner
func NewTransactionCleaner(
	dbManager *ChainDBManager,
	cfg *config.Config,
	logger zerolog.Logger,
) *TransactionCleaner {
	return &TransactionCleaner{
		dbManager:        dbManager,
		config:           cfg,
		cleanupInterval:  time.Duration(cfg.TransactionCleanupIntervalSeconds) * time.Second,
		retentionPeriod:  time.Duration(cfg.TransactionRetentionPeriodSeconds) * time.Second,
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

// performCleanup executes the actual cleanup of old confirmed transactions across all chain databases
func (tc *TransactionCleaner) performCleanup() error {
	start := time.Now()
	
	tc.logger.Debug().
		Dur("retention_period", tc.retentionPeriod).
		Msg("performing transaction cleanup across all chain databases")

	// Get all active databases
	databases := tc.dbManager.GetAllDatabases()
	if len(databases) == 0 {
		tc.logger.Debug().Msg("no active databases found - skipping cleanup")
		return nil
	}

	totalDeleted := int64(0)
	cleanupErrors := []error{}
	dbsCleaned := 0

	// Clean each database independently
	for chainID, chainDB := range databases {
		deletedCount, err := chainDB.DeleteOldConfirmedTransactions(tc.retentionPeriod)
		if err != nil {
			tc.logger.Error().
				Err(err).
				Str("chain_id", chainID).
				Msg("failed to cleanup transactions for chain")
			cleanupErrors = append(cleanupErrors, fmt.Errorf("chain %s: %w", chainID, err))
			continue
		}

		if deletedCount > 0 {
			tc.logger.Debug().
				Str("chain_id", chainID).
				Int64("deleted_count", deletedCount).
				Msg("cleaned transactions for chain")
			
			// Checkpoint WAL for this database
			tc.checkpointWALForDB(chainDB, chainID)
		}

		totalDeleted += deletedCount
		dbsCleaned++
	}

	duration := time.Since(start)
	
	if totalDeleted > 0 {
		tc.logger.Info().
			Int64("total_deleted", totalDeleted).
			Int("databases_cleaned", dbsCleaned).
			Int("total_databases", len(databases)).
			Dur("duration", duration).
			Msg("transaction cleanup completed")
	} else {
		tc.logger.Debug().
			Int("databases_checked", dbsCleaned).
			Dur("duration", duration).
			Msg("transaction cleanup completed - no transactions to delete")
	}

	// Return combined errors if any occurred
	if len(cleanupErrors) > 0 {
		return fmt.Errorf("cleanup failed for %d databases: %v", len(cleanupErrors), cleanupErrors)
	}

	return nil
}

// checkpointWALForDB performs WAL checkpointing for a specific database to prevent WAL file growth
func (tc *TransactionCleaner) checkpointWALForDB(database *DB, chainID string) {
	tc.logger.Debug().
		Str("chain_id", chainID).
		Msg("performing WAL checkpoint")
	
	// Use PRAGMA wal_checkpoint(TRUNCATE) to force a checkpoint and truncate the WAL
	if err := database.Client().Exec("PRAGMA wal_checkpoint(TRUNCATE)").Error; err != nil {
		tc.logger.Warn().
			Err(err).
			Str("chain_id", chainID).
			Msg("failed to checkpoint WAL")
	} else {
		tc.logger.Debug().
			Str("chain_id", chainID).
			Msg("WAL checkpoint completed")
	}
}