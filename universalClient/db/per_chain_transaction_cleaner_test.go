package db

import (
	"context"
	"testing"
	"time"

	"github.com/rollchains/pchain/universalClient/config"
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