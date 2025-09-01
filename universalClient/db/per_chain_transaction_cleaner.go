package db

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/rollchains/pchain/universalClient/config"
)

// chainCleaner handles cleanup for a single chain
type chainCleaner struct {
	chainID         string
	database        *DB
	ticker          *time.Ticker
	stopCh          chan struct{}
	cleanupInterval time.Duration
	retentionPeriod time.Duration
	logger          zerolog.Logger
}

// PerChainTransactionCleaner handles periodic cleanup of old confirmed transactions with per-chain configuration
type PerChainTransactionCleaner struct {
	dbManager      *ChainDBManager
	config         *config.Config
	chainCleaners  map[string]*chainCleaner
	mu             sync.RWMutex
	logger         zerolog.Logger
	ctx            context.Context
	cancel         context.CancelFunc
}

// NewPerChainTransactionCleaner creates a new per-chain transaction cleaner
func NewPerChainTransactionCleaner(
	dbManager *ChainDBManager,
	cfg *config.Config,
	logger zerolog.Logger,
) *PerChainTransactionCleaner {
	ctx, cancel := context.WithCancel(context.Background())
	
	return &PerChainTransactionCleaner{
		dbManager:     dbManager,
		config:        cfg,
		chainCleaners: make(map[string]*chainCleaner),
		logger:        logger.With().Str("component", "per_chain_transaction_cleaner").Logger(),
		ctx:           ctx,
		cancel:        cancel,
	}
}

// Start begins the per-chain cleanup process
func (tc *PerChainTransactionCleaner) Start(ctx context.Context) error {
	tc.logger.Info().Msg("starting per-chain transaction cleaner")

	// Get all active databases and start cleaners for each
	databases := tc.dbManager.GetAllDatabases()
	
	for chainID, chainDB := range databases {
		if err := tc.startChainCleaner(chainID, chainDB); err != nil {
			tc.logger.Error().
				Err(err).
				Str("chain_id", chainID).
				Msg("failed to start cleaner for chain")
			// Continue with other chains even if one fails
		}
	}

	// Monitor for new chains being added
	go tc.monitorChainUpdates(ctx)

	return nil
}

// startChainCleaner starts a cleaner for a specific chain
func (tc *PerChainTransactionCleaner) startChainCleaner(chainID string, database *DB) error {
	tc.mu.Lock()
	defer tc.mu.Unlock()

	// Check if cleaner already exists for this chain
	if _, exists := tc.chainCleaners[chainID]; exists {
		tc.logger.Debug().
			Str("chain_id", chainID).
			Msg("cleaner already exists for chain")
		return nil
	}

	// Get chain-specific settings (with fallback to global defaults)
	cleanupInterval, retentionPeriod := tc.config.GetChainCleanupSettings(chainID)

	cleaner := &chainCleaner{
		chainID:         chainID,
		database:        database,
		cleanupInterval: time.Duration(cleanupInterval) * time.Second,
		retentionPeriod: time.Duration(retentionPeriod) * time.Second,
		stopCh:          make(chan struct{}),
		logger: tc.logger.With().
			Str("chain_id", chainID).
			Logger(),
	}

	tc.logger.Info().
		Str("chain_id", chainID).
		Str("cleanup_interval", cleaner.cleanupInterval.String()).
		Str("retention_period", cleaner.retentionPeriod.String()).
		Msg("starting cleaner for chain")

	// Perform initial cleanup
	if err := tc.performChainCleanup(cleaner); err != nil {
		cleaner.logger.Error().Err(err).Msg("failed to perform initial cleanup")
		// Don't fail startup on cleanup error, just log it
	}

	// Start periodic cleanup
	cleaner.ticker = time.NewTicker(cleaner.cleanupInterval)
	
	go func() {
		defer cleaner.ticker.Stop()
		for {
			select {
			case <-tc.ctx.Done():
				cleaner.logger.Info().Msg("context cancelled, stopping chain cleaner")
				return
			case <-cleaner.stopCh:
				cleaner.logger.Info().Msg("stop signal received, stopping chain cleaner")
				return
			case <-cleaner.ticker.C:
				if err := tc.performChainCleanup(cleaner); err != nil {
					cleaner.logger.Error().Err(err).Msg("failed to perform scheduled cleanup")
				}
			}
		}
	}()

	tc.chainCleaners[chainID] = cleaner
	return nil
}

// performChainCleanup executes cleanup for a specific chain
func (tc *PerChainTransactionCleaner) performChainCleanup(cleaner *chainCleaner) error {
	start := time.Now()
	
	cleaner.logger.Debug().
		Str("retention_period", cleaner.retentionPeriod.String()).
		Msg("performing transaction cleanup for chain")

	deletedCount, err := cleaner.database.DeleteOldConfirmedTransactions(cleaner.retentionPeriod)
	if err != nil {
		return fmt.Errorf("failed to cleanup transactions: %w", err)
	}

	duration := time.Since(start)

	if deletedCount > 0 {
		cleaner.logger.Info().
			Int64("deleted_count", deletedCount).
			Str("duration", duration.String()).
			Msg("transaction cleanup completed for chain")
		
		// Checkpoint WAL after cleanup
		tc.checkpointWALForDB(cleaner.database, cleaner.chainID)
	} else {
		cleaner.logger.Debug().
			Str("duration", duration.String()).
			Msg("transaction cleanup completed - no transactions to delete")
	}

	return nil
}

// checkpointWALForDB performs WAL checkpointing for a specific database
func (tc *PerChainTransactionCleaner) checkpointWALForDB(database *DB, chainID string) {
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

// monitorChainUpdates monitors for new chains being added and starts cleaners for them
func (tc *PerChainTransactionCleaner) monitorChainUpdates(ctx context.Context) {
	// Check for new chains every minute
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-tc.ctx.Done():
			return
		case <-ticker.C:
			tc.checkForNewChains()
		}
	}
}

// checkForNewChains checks for new chains and starts cleaners for them
func (tc *PerChainTransactionCleaner) checkForNewChains() {
	databases := tc.dbManager.GetAllDatabases()
	
	for chainID, chainDB := range databases {
		tc.mu.RLock()
		_, exists := tc.chainCleaners[chainID]
		tc.mu.RUnlock()
		
		if !exists {
			tc.logger.Info().
				Str("chain_id", chainID).
				Msg("detected new chain, starting cleaner")
			
			if err := tc.startChainCleaner(chainID, chainDB); err != nil {
				tc.logger.Error().
					Err(err).
					Str("chain_id", chainID).
					Msg("failed to start cleaner for new chain")
			}
		}
	}
}

// UpdateChainConfig updates the configuration for a specific chain's cleaner
func (tc *PerChainTransactionCleaner) UpdateChainConfig(chainID string) {
	tc.mu.Lock()
	defer tc.mu.Unlock()

	cleaner, exists := tc.chainCleaners[chainID]
	if !exists {
		tc.logger.Debug().
			Str("chain_id", chainID).
			Msg("no cleaner exists for chain, skipping config update")
		return
	}

	// Get updated settings
	newCleanupInterval, newRetentionPeriod := tc.config.GetChainCleanupSettings(chainID)

	// Check if settings have changed
	if time.Duration(newCleanupInterval)*time.Second == cleaner.cleanupInterval &&
		time.Duration(newRetentionPeriod)*time.Second == cleaner.retentionPeriod {
		return // No changes
	}

	tc.logger.Info().
		Str("chain_id", chainID).
		Str("old_cleanup_interval", cleaner.cleanupInterval.String()).
		Str("new_cleanup_interval", (time.Duration(newCleanupInterval) * time.Second).String()).
		Str("old_retention_period", cleaner.retentionPeriod.String()).
		Str("new_retention_period", (time.Duration(newRetentionPeriod) * time.Second).String()).
		Msg("updating cleaner configuration for chain")

	// Stop the old cleaner
	close(cleaner.stopCh)
	if cleaner.ticker != nil {
		cleaner.ticker.Stop()
	}

	// Remove from map
	delete(tc.chainCleaners, chainID)

	// Get the database for this chain
	databases := tc.dbManager.GetAllDatabases()
	if chainDB, ok := databases[chainID]; ok {
		// Start a new cleaner with updated settings
		tc.mu.Unlock() // Unlock before calling startChainCleaner to avoid deadlock
		if err := tc.startChainCleaner(chainID, chainDB); err != nil {
			tc.logger.Error().
				Err(err).
				Str("chain_id", chainID).
				Msg("failed to restart cleaner with updated config")
		}
		tc.mu.Lock() // Re-lock for consistency, even though we're about to return
	}
}

// Stop gracefully stops all chain cleaners
func (tc *PerChainTransactionCleaner) Stop() {
	tc.logger.Info().Msg("stopping per-chain transaction cleaner")
	
	// Cancel the context to stop monitoring
	tc.cancel()

	tc.mu.Lock()
	defer tc.mu.Unlock()

	// Stop all chain cleaners
	for chainID, cleaner := range tc.chainCleaners {
		tc.logger.Debug().
			Str("chain_id", chainID).
			Msg("stopping cleaner for chain")
		
		close(cleaner.stopCh)
		if cleaner.ticker != nil {
			cleaner.ticker.Stop()
		}
	}

	// Clear the map
	tc.chainCleaners = make(map[string]*chainCleaner)
}

// GetCleanerStatus returns the status of all chain cleaners
func (tc *PerChainTransactionCleaner) GetCleanerStatus() map[string]struct {
	CleanupInterval time.Duration
	RetentionPeriod time.Duration
} {
	tc.mu.RLock()
	defer tc.mu.RUnlock()

	status := make(map[string]struct {
		CleanupInterval time.Duration
		RetentionPeriod time.Duration
	})

	for chainID, cleaner := range tc.chainCleaners {
		status[chainID] = struct {
			CleanupInterval time.Duration
			RetentionPeriod time.Duration
		}{
			CleanupInterval: cleaner.cleanupInterval,
			RetentionPeriod: cleaner.retentionPeriod,
		}
	}

	return status
}