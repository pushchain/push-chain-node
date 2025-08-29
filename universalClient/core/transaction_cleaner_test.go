package core

import (
	"context"
	"testing"
	"time"

	"github.com/rollchains/pchain/universalClient/config"
	"github.com/rollchains/pchain/universalClient/db"
	"github.com/rollchains/pchain/universalClient/logger"
	"github.com/rollchains/pchain/universalClient/store"
	"github.com/stretchr/testify/require"
)

func TestTransactionCleaner(t *testing.T) {
	// Setup test database
	database, err := db.OpenInMemoryDB(true)
	require.NoError(t, err)
	defer database.Close()

	// Setup test config
	cfg := &config.Config{
		TransactionCleanupInterval: 100 * time.Millisecond,
		TransactionRetentionPeriod: time.Hour,
		LogLevel:                   0, // Debug level
		LogFormat:                  "console",
	}

	// Setup logger
	log := logger.Init(*cfg)

	// Create test transactions
	now := time.Now()
	
	// Old confirmed transaction (should be deleted)
	oldConfirmed := &store.GatewayTransaction{
		ChainID:         "eip155:1",
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
	recentConfirmed := &store.GatewayTransaction{
		ChainID:         "eip155:1",
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
	oldPending := &store.GatewayTransaction{
		ChainID:         "eip155:1",
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
	require.NoError(t, database.Client().Model(&store.GatewayTransaction{}).Count(&count).Error)
	require.Equal(t, int64(3), count)

	t.Run("DeleteOldConfirmedTransactions", func(t *testing.T) {
		// Test the database method directly
		deletedCount, err := database.DeleteOldConfirmedTransactions(cfg.TransactionRetentionPeriod)
		require.NoError(t, err)
		require.Equal(t, int64(1), deletedCount) // Only old confirmed should be deleted

		// Verify remaining transactions
		var remaining []store.GatewayTransaction
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
	newOldConfirmed := &store.GatewayTransaction{
		ChainID:         "eip155:1",
		TxHash:          "0x111_new", // Use different hash to avoid constraint violation
		BlockNumber:     100,
		Method:          "deposit",
		EventIdentifier: "event1_new",
		Status:          "confirmed",
		Confirmations:   15,
	}
	require.NoError(t, database.Client().Create(newOldConfirmed).Error)
	require.NoError(t, database.Client().Model(newOldConfirmed).Update("updated_at", oldTime).Error)

	t.Run("TransactionCleanerService", func(t *testing.T) {
		// Create transaction cleaner
		cleaner := NewTransactionCleaner(database, cfg, log)
		
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
		require.NoError(t, database.Client().Model(&store.GatewayTransaction{}).Count(&finalCount).Error)
		require.Equal(t, int64(2), finalCount) // Should have 2 transactions left

		// Verify the correct transactions remain
		var final []store.GatewayTransaction
		require.NoError(t, database.Client().Find(&final).Error)
		
		txHashes := make(map[string]bool)
		for _, tx := range final {
			txHashes[tx.TxHash] = true
		}
		require.True(t, txHashes["0x222"])  // Recent confirmed should remain
		require.True(t, txHashes["0x333"])  // Old pending should remain
	})
}

func TestTransactionCleanerEdgeCases(t *testing.T) {
	// Setup test database
	database, err := db.OpenInMemoryDB(true)
	require.NoError(t, err)
	defer database.Close()

	// Setup test config
	cfg := &config.Config{
		TransactionCleanupInterval: 50 * time.Millisecond,
		TransactionRetentionPeriod: time.Hour,
		LogLevel:                   0,
		LogFormat:                  "console",
	}

	_ = logger.Init(*cfg)

	t.Run("EmptyDatabase", func(t *testing.T) {
		// Test cleanup with no transactions
		deletedCount, err := database.DeleteOldConfirmedTransactions(cfg.TransactionRetentionPeriod)
		require.NoError(t, err)
		require.Equal(t, int64(0), deletedCount)
	})

	t.Run("OnlyRecentTransactions", func(t *testing.T) {
		// Create only recent transactions
		recent := &store.GatewayTransaction{
			ChainID:         "eip155:1",
			TxHash:          "0x456",
			BlockNumber:     300,
			Method:          "withdraw",
			EventIdentifier: "event4",
			Status:          "confirmed",
			Confirmations:   12,
		}
		require.NoError(t, database.Client().Create(recent).Error)

		deletedCount, err := database.DeleteOldConfirmedTransactions(cfg.TransactionRetentionPeriod)
		require.NoError(t, err)
		require.Equal(t, int64(0), deletedCount) // Nothing should be deleted

		// Verify transaction still exists
		var count int64
		require.NoError(t, database.Client().Model(&store.GatewayTransaction{}).Count(&count).Error)
		require.Equal(t, int64(1), count)
	})

	t.Run("DifferentStatuses", func(t *testing.T) {
		// Clean up from previous test
		database.Client().Exec("DELETE FROM gateway_transactions")

		now := time.Now()
		oldTime := now.Add(-25 * time.Hour)

		// Create transactions with different statuses, all old
		statuses := []string{"pending", "fast_confirmed", "confirmed", "failed", "reorged"}
		
		for i, status := range statuses {
			tx := &store.GatewayTransaction{
				ChainID:         "eip155:1",
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
		deletedCount, err := database.DeleteOldConfirmedTransactions(cfg.TransactionRetentionPeriod)
		require.NoError(t, err)
		require.Equal(t, int64(1), deletedCount) // Only the "confirmed" one

		// Verify remaining transactions
		var remaining []store.GatewayTransaction
		require.NoError(t, database.Client().Find(&remaining).Error)
		require.Len(t, remaining, 4) // All except "confirmed"

		for _, tx := range remaining {
			require.NotEqual(t, "confirmed", tx.Status)
		}
	})
}

func TestTransactionCleanerConfiguration(t *testing.T) {
	database, err := db.OpenInMemoryDB(true)
	require.NoError(t, err)
	defer database.Close()

	cfg := &config.Config{
		TransactionCleanupInterval: 200 * time.Millisecond,
		TransactionRetentionPeriod: 30 * time.Minute, // Shorter retention period
		LogLevel:                   1,
		LogFormat:                  "json",
	}

	log := logger.Init(*cfg)

	cleaner := NewTransactionCleaner(database, cfg, log)

	// Verify configuration is properly set
	require.Equal(t, cfg.TransactionCleanupInterval, cleaner.cleanupInterval)
	require.Equal(t, cfg.TransactionRetentionPeriod, cleaner.retentionPeriod)
}