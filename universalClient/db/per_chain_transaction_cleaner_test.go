package db

import (
	"context"
	"testing"
	"time"

	"github.com/rollchains/pchain/universalClient/config"
	"github.com/rollchains/pchain/universalClient/logger"
	"github.com/rollchains/pchain/universalClient/store"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPerChainTransactionCleaner(t *testing.T) {
	log := zerolog.New(zerolog.NewTestWriter(t))

	t.Run("GetChainCleanupSettings", func(t *testing.T) {
		// Test with chain-specific configuration
		cfg := &config.Config{
			TransactionCleanupIntervalSeconds: 3600,  // Global default: 1 hour
			TransactionRetentionPeriodSeconds: 86400, // Global default: 24 hours
			ChainConfigs: map[string]config.ChainSpecificConfig{
				"eip155:11155111": {
					CleanupIntervalSeconds: intPtr(1800),  // Chain-specific: 30 minutes
					RetentionPeriodSeconds: intPtr(43200), // Chain-specific: 12 hours
				},
				"solana:devnet": {
					CleanupIntervalSeconds: intPtr(7200), // Chain-specific: 2 hours
					// RetentionPeriodSeconds not set, should use global default
				},
			},
		}

		// Test chain with full override
		cleanup, retention := cfg.GetChainCleanupSettings("eip155:11155111")
		assert.Equal(t, 1800, cleanup, "should use chain-specific cleanup interval")
		assert.Equal(t, 43200, retention, "should use chain-specific retention period")

		// Test chain with partial override
		cleanup, retention = cfg.GetChainCleanupSettings("solana:devnet")
		assert.Equal(t, 7200, cleanup, "should use chain-specific cleanup interval")
		assert.Equal(t, 86400, retention, "should use global default retention period")

		// Test chain without specific config (uses global defaults)
		cleanup, retention = cfg.GetChainCleanupSettings("unknown:chain")
		assert.Equal(t, 3600, cleanup, "should use global default cleanup interval")
		assert.Equal(t, 86400, retention, "should use global default retention period")

		// Test with nil ChainConfigs
		cfgNoChainConfig := &config.Config{
			TransactionCleanupIntervalSeconds: 3600,
			TransactionRetentionPeriodSeconds: 86400,
			ChainConfigs:                      nil,
		}
		cleanup, retention = cfgNoChainConfig.GetChainCleanupSettings("any:chain")
		assert.Equal(t, 3600, cleanup, "should use global default when no chain config exists")
		assert.Equal(t, 86400, retention, "should use global default when no chain config exists")
	})

	t.Run("PerChainCleaner_Creation", func(t *testing.T) {
		tempDir := t.TempDir()
		
		cfg := &config.Config{
			DatabaseBaseDir:                   tempDir,
			TransactionCleanupIntervalSeconds: 3600,
			TransactionRetentionPeriodSeconds: 86400,
			ChainConfigs: map[string]config.ChainSpecificConfig{
				"eip155:11155111": {
					CleanupIntervalSeconds: intPtr(1800),
					RetentionPeriodSeconds: intPtr(43200),
				},
			},
		}

		dbManager := NewChainDBManager(tempDir, log, cfg)
		defer dbManager.CloseAll()

		cleaner := NewPerChainTransactionCleaner(dbManager, cfg, log)
		require.NotNil(t, cleaner)

		// Test that the cleaner is properly initialized
		assert.NotNil(t, cleaner.dbManager)
		assert.NotNil(t, cleaner.config)
		assert.NotNil(t, cleaner.chainCleaners)
		assert.NotNil(t, cleaner.ctx)
		assert.NotNil(t, cleaner.cancel)
	})

	t.Run("PerChainCleaner_StartStop", func(t *testing.T) {
		tempDir := t.TempDir()
		
		cfg := &config.Config{
			DatabaseBaseDir:                   tempDir,
			TransactionCleanupIntervalSeconds: 1, // Very short for testing
			TransactionRetentionPeriodSeconds: 1,
			ChainConfigs: map[string]config.ChainSpecificConfig{
				"eip155:11155111": {
					CleanupIntervalSeconds: intPtr(1),
					RetentionPeriodSeconds: intPtr(1),
				},
			},
		}

		dbManager := NewChainDBManager(tempDir, log, cfg)
		defer dbManager.CloseAll()

		// Create databases for testing
		db1, err := dbManager.GetChainDB("eip155:11155111")
		require.NoError(t, err)
		require.NotNil(t, db1)

		db2, err := dbManager.GetChainDB("solana:devnet")
		require.NoError(t, err)
		require.NotNil(t, db2)

		cleaner := NewPerChainTransactionCleaner(dbManager, cfg, log)

		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		// Start the cleaner
		err = cleaner.Start(ctx)
		require.NoError(t, err)

		// Give it time to start cleaners
		time.Sleep(100 * time.Millisecond)

		// Check that cleaners were created for existing databases
		status := cleaner.GetCleanerStatus()
		assert.Len(t, status, 2, "should have cleaners for both chains")

		// Verify chain-specific settings are applied
		if eth, ok := status["eip155:11155111"]; ok {
			assert.Equal(t, 1*time.Second, eth.CleanupInterval)
			assert.Equal(t, 1*time.Second, eth.RetentionPeriod)
		}

		// Verify global defaults are used for chain without specific config
		if sol, ok := status["solana:devnet"]; ok {
			assert.Equal(t, 1*time.Second, sol.CleanupInterval)
			assert.Equal(t, 1*time.Second, sol.RetentionPeriod)
		}

		// Stop the cleaner
		cleaner.Stop()

		// Verify all cleaners are stopped
		status = cleaner.GetCleanerStatus()
		assert.Len(t, status, 0, "all cleaners should be stopped")
	})

	t.Run("PerChainCleaner_DynamicChainAddition", func(t *testing.T) {
		tempDir := t.TempDir()
		
		cfg := &config.Config{
			DatabaseBaseDir:                   tempDir,
			TransactionCleanupIntervalSeconds: 2,
			TransactionRetentionPeriodSeconds: 2,
		}

		dbManager := NewChainDBManager(tempDir, log, cfg)
		defer dbManager.CloseAll()

		cleaner := NewPerChainTransactionCleaner(dbManager, cfg, log)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// Start with no databases
		err := cleaner.Start(ctx)
		require.NoError(t, err)

		// Initially should have no cleaners
		status := cleaner.GetCleanerStatus()
		assert.Len(t, status, 0, "should have no cleaners initially")

		// Add a new database
		db1, err := dbManager.GetChainDB("eip155:11155111")
		require.NoError(t, err)
		require.NotNil(t, db1)

		// Manually trigger check for new chains (normally done periodically)
		cleaner.checkForNewChains()

		// Should now have a cleaner for the new chain
		status = cleaner.GetCleanerStatus()
		assert.Len(t, status, 1, "should have cleaner for new chain")

		// Stop the cleaner
		cleaner.Stop()
	})

	t.Run("PerChainCleaner_ConfigUpdate", func(t *testing.T) {
		tempDir := t.TempDir()
		
		cfg := &config.Config{
			DatabaseBaseDir:                   tempDir,
			TransactionCleanupIntervalSeconds: 3600,
			TransactionRetentionPeriodSeconds: 86400,
			ChainConfigs: map[string]config.ChainSpecificConfig{
				"eip155:11155111": {
					CleanupIntervalSeconds: intPtr(1800),
					RetentionPeriodSeconds: intPtr(43200),
				},
			},
		}

		dbManager := NewChainDBManager(tempDir, log, cfg)
		defer dbManager.CloseAll()

		// Create database
		db1, err := dbManager.GetChainDB("eip155:11155111")
		require.NoError(t, err)
		require.NotNil(t, db1)

		cleaner := NewPerChainTransactionCleaner(dbManager, cfg, log)

		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		// Start the cleaner
		err = cleaner.Start(ctx)
		require.NoError(t, err)

		// Verify initial settings
		status := cleaner.GetCleanerStatus()
		if eth, ok := status["eip155:11155111"]; ok {
			assert.Equal(t, 30*time.Minute, eth.CleanupInterval)
			assert.Equal(t, 12*time.Hour, eth.RetentionPeriod)
		}

		// Update configuration
		cfg.ChainConfigs["eip155:11155111"] = config.ChainSpecificConfig{
			CleanupIntervalSeconds: intPtr(900),   // 15 minutes
			RetentionPeriodSeconds: intPtr(21600), // 6 hours
		}

		// Update the cleaner configuration
		cleaner.UpdateChainConfig("eip155:11155111")

		// Give it time to restart
		time.Sleep(100 * time.Millisecond)

		// Verify updated settings
		status = cleaner.GetCleanerStatus()
		if eth, ok := status["eip155:11155111"]; ok {
			assert.Equal(t, 15*time.Minute, eth.CleanupInterval)
			assert.Equal(t, 6*time.Hour, eth.RetentionPeriod)
		}

		// Stop the cleaner
		cleaner.Stop()
	})
}

// Helper function to create int pointers
func intPtr(i int) *int {
	return &i
}

// TestPerChainTransactionCleanerDatabaseOperations tests actual database cleanup functionality
func TestPerChainTransactionCleanerDatabaseOperations(t *testing.T) {
	// Setup test config
	cfg := &config.Config{
		TransactionCleanupIntervalSeconds: 1, // 1 second for testing
		TransactionRetentionPeriodSeconds: 3600, // 1 hour
		LogLevel:                          0, // Debug level
		LogFormat:                         "console",
	}

	// Setup logger
	log := logger.Init(*cfg)

	// Setup ChainDBManager
	dbManager := NewInMemoryChainDBManager(log, cfg)
	defer dbManager.CloseAll()

	// Get database for test chain
	chainID := "eip155:1"
	database, err := dbManager.GetChainDB(chainID)
	require.NoError(t, err)

	// Create test transactions
	now := time.Now()
	
	// Old confirmed transaction (should be deleted)
	oldConfirmed := &store.ChainTransaction{
		TxHash:          "0x111",
		BlockNumber:     100,
		Method:          "deposit",
		EventIdentifier: "event1",
		Status:          "confirmed",
		Confirmations:   15,
	}
	// Set UpdatedAt to 25 hours ago (older than retention period)
	oldTime := now.Add(-25 * time.Hour)
	
	// Recent confirmed transaction (should NOT be deleted)
	recentConfirmed := &store.ChainTransaction{
		TxHash:          "0x222",
		BlockNumber:     200,
		Method:          "deposit",
		EventIdentifier: "event2",
		Status:          "confirmed",
		Confirmations:   10,
	}
	// Set UpdatedAt to 30 minutes ago (clearly within retention period)
	recentTime := now.Add(-30 * time.Minute)
	
	// Old pending transaction (should NOT be deleted regardless of age)
	oldPending := &store.ChainTransaction{
		TxHash:          "0x333",
		BlockNumber:     150,
		Method:          "deposit",
		EventIdentifier: "event3",
		Status:          "pending",
		Confirmations:   5,
	}

	// Insert test transactions
	require.NoError(t, database.Client().Create(oldConfirmed).Error)
	require.NoError(t, database.Client().Create(recentConfirmed).Error)
	require.NoError(t, database.Client().Create(oldPending).Error)

	// Manually set the UpdatedAt timestamps since GORM auto-sets them
	require.NoError(t, database.Client().Model(oldConfirmed).Update("updated_at", oldTime).Error)
	require.NoError(t, database.Client().Model(recentConfirmed).Update("updated_at", recentTime).Error)
	require.NoError(t, database.Client().Model(oldPending).Update("updated_at", oldTime).Error)

	// Verify initial state
	var count int64
	require.NoError(t, database.Client().Model(&store.ChainTransaction{}).Count(&count).Error)
	require.Equal(t, int64(3), count)

	t.Run("DeleteOldConfirmedTransactions", func(t *testing.T) {
		// Test the database method directly
		deletedCount, err := database.DeleteOldConfirmedTransactions(time.Duration(cfg.TransactionRetentionPeriodSeconds) * time.Second)
		require.NoError(t, err)
		require.Equal(t, int64(1), deletedCount) // Only old confirmed should be deleted

		// Verify remaining transactions
		var remaining []store.ChainTransaction
		require.NoError(t, database.Client().Find(&remaining).Error)
		require.Len(t, remaining, 2)

		// Check that the right transactions remain
		txHashes := make(map[string]bool)
		for _, tx := range remaining {
			txHashes[tx.TxHash] = true
		}
		require.True(t, txHashes["0x222"])  // Recent confirmed should remain
		require.True(t, txHashes["0x333"])  // Old pending should remain
		require.False(t, txHashes["0x111"]) // Old confirmed should be gone
	})

	// Create a new old confirmed transaction for the cleaner service test
	newOldConfirmed := &store.ChainTransaction{
		
		TxHash:          "0x111_new", // Use different hash to avoid constraint violation
		BlockNumber:     100,
		Method:          "deposit",
		EventIdentifier: "event1_new",
		Status:          "confirmed",
		Confirmations:   15,
	}
	require.NoError(t, database.Client().Create(newOldConfirmed).Error)
	require.NoError(t, database.Client().Model(newOldConfirmed).Update("updated_at", oldTime).Error)

	t.Run("PerChainTransactionCleanerService", func(t *testing.T) {
		// Create per-chain transaction cleaner
		cleaner := NewPerChainTransactionCleaner(dbManager, cfg, log)
		
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// Start the cleaner
		require.NoError(t, cleaner.Start(ctx))

		// Wait for at least one cleanup cycle
		time.Sleep(200 * time.Millisecond)

		// Stop the cleaner
		cleaner.Stop()

		// Verify cleanup occurred
		var finalCount int64
		require.NoError(t, database.Client().Model(&store.ChainTransaction{}).Count(&finalCount).Error)
		require.Equal(t, int64(2), finalCount) // Should have 2 transactions left

		// Verify the correct transactions remain
		var final []store.ChainTransaction
		require.NoError(t, database.Client().Find(&final).Error)
		
		txHashes := make(map[string]bool)
		for _, tx := range final {
			txHashes[tx.TxHash] = true
		}
		require.True(t, txHashes["0x222"])  // Recent confirmed should remain
		require.True(t, txHashes["0x333"])  // Old pending should remain
	})
}

// TestPerChainTransactionCleanerEdgeCases tests edge cases for transaction cleanup
func TestPerChainTransactionCleanerEdgeCases(t *testing.T) {
	// Setup test config
	cfg := &config.Config{
		TransactionCleanupIntervalSeconds: 1,    // 1 second for testing
		TransactionRetentionPeriodSeconds: 3600, // 1 hour
		LogLevel:                   0,
		LogFormat:                  "console",
	}

	log := logger.Init(*cfg)

	// Setup ChainDBManager
	dbManager := NewInMemoryChainDBManager(log, cfg)
	defer dbManager.CloseAll()

	// Get database for test chain
	chainID := "eip155:1"
	database, err := dbManager.GetChainDB(chainID)
	require.NoError(t, err)

	t.Run("EmptyDatabase", func(t *testing.T) {
		// Test cleanup with no transactions
		deletedCount, err := database.DeleteOldConfirmedTransactions(time.Duration(cfg.TransactionRetentionPeriodSeconds) * time.Second)
		require.NoError(t, err)
		require.Equal(t, int64(0), deletedCount)
	})

	t.Run("OnlyRecentTransactions", func(t *testing.T) {
		// Create only recent transactions
		recent := &store.ChainTransaction{
			TxHash:          "0x456",
			BlockNumber:     300,
			Method:          "withdraw",
			EventIdentifier: "event4",
			Status:          "confirmed",
			Confirmations:   12,
		}
		require.NoError(t, database.Client().Create(recent).Error)

		deletedCount, err := database.DeleteOldConfirmedTransactions(time.Duration(cfg.TransactionRetentionPeriodSeconds) * time.Second)
		require.NoError(t, err)
		require.Equal(t, int64(0), deletedCount) // Nothing should be deleted

		// Verify transaction still exists
		var count int64
		require.NoError(t, database.Client().Model(&store.ChainTransaction{}).Count(&count).Error)
		require.Equal(t, int64(1), count)
	})

	t.Run("DifferentStatuses", func(t *testing.T) {
		// Clean up from previous test
		database.Client().Exec("DELETE FROM chain_transactions")

		now := time.Now()
		oldTime := now.Add(-25 * time.Hour)

		// Create transactions with different statuses, all old
		statuses := []string{"pending", "fast_confirmed", "confirmed", "failed", "reorged"}
		
		for i, status := range statuses {
			tx := &store.ChainTransaction{
				TxHash:          string(rune('a' + i)) + "00",
				BlockNumber:     uint64(400 + i),
				Method:          "test",
				EventIdentifier: "event" + string(rune('5' + i)),
				Status:          status,
				Confirmations:   10,
			}
			require.NoError(t, database.Client().Create(tx).Error)
			require.NoError(t, database.Client().Model(tx).Update("updated_at", oldTime).Error)
		}

		// Only "confirmed" should be deleted
		deletedCount, err := database.DeleteOldConfirmedTransactions(time.Duration(cfg.TransactionRetentionPeriodSeconds) * time.Second)
		require.NoError(t, err)
		require.Equal(t, int64(1), deletedCount) // Only the "confirmed" one

		// Verify remaining transactions
		var remaining []store.ChainTransaction
		require.NoError(t, database.Client().Find(&remaining).Error)
		require.Len(t, remaining, 4) // All except "confirmed"

		for _, tx := range remaining {
			require.NotEqual(t, "confirmed", tx.Status)
		}
	})
}