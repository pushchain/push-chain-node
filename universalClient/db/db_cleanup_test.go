package db

import (
	"testing"
	"time"

	"github.com/rollchains/pchain/universalClient/store"
	"github.com/stretchr/testify/require"
)

func TestDeleteOldConfirmedTransactions(t *testing.T) {
	// Setup in-memory test database
	database, err := OpenInMemoryDB(true)
	require.NoError(t, err)
	defer database.Close()

	now := time.Now()
	retentionPeriod := 24 * time.Hour

	// Create test transactions
	testCases := []struct {
		name     string
		txHash   string
		status   string
		age      time.Duration
		shouldDelete bool
	}{
		{
			name:         "old confirmed",
			txHash:       "0x1111",
			status:       "confirmed",
			age:          25 * time.Hour, // Older than retention period
			shouldDelete: true,
		},
		{
			name:         "recent confirmed",
			txHash:       "0x2222",
			status:       "confirmed",
			age:          23 * time.Hour, // Within retention period
			shouldDelete: false,
		},
		{
			name:         "old pending",
			txHash:       "0x3333",
			status:       "pending",
			age:          25 * time.Hour, // Older than retention period but pending
			shouldDelete: false,
		},
		{
			name:         "old fast_confirmed",
			txHash:       "0x4444",
			status:       "fast_confirmed",
			age:          25 * time.Hour, // Older than retention period but not "confirmed"
			shouldDelete: false,
		},
		{
			name:         "old failed",
			txHash:       "0x5555",
			status:       "failed",
			age:          25 * time.Hour, // Older than retention period but failed
			shouldDelete: false,
		},
		{
			name:         "old reorged",
			txHash:       "0x6666",
			status:       "reorged",
			age:          25 * time.Hour, // Older than retention period but reorged
			shouldDelete: false,
		},
	}

	// Insert test transactions
	var insertedTransactions []*store.GatewayTransaction
	for i, tc := range testCases {
		tx := &store.GatewayTransaction{
			ChainID:         "eip155:1",
			TxHash:          tc.txHash,
			BlockNumber:     uint64(100 + i),
			Method:          "deposit",
			EventIdentifier: "event_" + tc.txHash,
			Status:          tc.status,
			Confirmations:   10,
		}
		
		require.NoError(t, database.Client().Create(tx).Error)
		
		// Set the UpdatedAt timestamp manually to simulate age
		targetTime := now.Add(-tc.age)
		require.NoError(t, database.Client().Model(tx).Update("updated_at", targetTime).Error)
		
		insertedTransactions = append(insertedTransactions, tx)
	}

	// Verify initial count
	var initialCount int64
	require.NoError(t, database.Client().Model(&store.GatewayTransaction{}).Count(&initialCount).Error)
	require.Equal(t, int64(len(testCases)), initialCount)

	t.Run("DeleteOldConfirmedTransactions", func(t *testing.T) {
		// Perform deletion
		deletedCount, err := database.DeleteOldConfirmedTransactions(retentionPeriod)
		require.NoError(t, err)

		// Count expected deletions
		expectedDeleted := 0
		for _, tc := range testCases {
			if tc.shouldDelete {
				expectedDeleted++
			}
		}
		require.Equal(t, int64(expectedDeleted), deletedCount)

		// Verify remaining transactions
		var remaining []store.GatewayTransaction
		require.NoError(t, database.Client().Find(&remaining).Error)

		expectedRemaining := len(testCases) - expectedDeleted
		require.Len(t, remaining, expectedRemaining)

		// Verify correct transactions remain
		remainingHashes := make(map[string]bool)
		for _, tx := range remaining {
			remainingHashes[tx.TxHash] = true
		}

		for _, tc := range testCases {
			if tc.shouldDelete {
				require.False(t, remainingHashes[tc.txHash], "Transaction %s should have been deleted", tc.name)
			} else {
				require.True(t, remainingHashes[tc.txHash], "Transaction %s should not have been deleted", tc.name)
			}
		}
	})
}

func TestDeleteOldConfirmedTransactionsEdgeCases(t *testing.T) {
	database, err := OpenInMemoryDB(true)
	require.NoError(t, err)
	defer database.Close()

	t.Run("EmptyDatabase", func(t *testing.T) {
		deletedCount, err := database.DeleteOldConfirmedTransactions(24 * time.Hour)
		require.NoError(t, err)
		require.Equal(t, int64(0), deletedCount)
	})

	t.Run("NoMatchingTransactions", func(t *testing.T) {
		// Insert only recent or non-confirmed transactions
		recentConfirmed := &store.GatewayTransaction{
			ChainID:         "eip155:1",
			TxHash:          "0x7777",
			BlockNumber:     500,
			Method:          "test",
			EventIdentifier: "recent",
			Status:          "confirmed",
			Confirmations:   12,
		}
		require.NoError(t, database.Client().Create(recentConfirmed).Error)

		oldPending := &store.GatewayTransaction{
			ChainID:         "eip155:1", 
			TxHash:          "0x8888",
			BlockNumber:     501,
			Method:          "test",
			EventIdentifier: "old_pending",
			Status:          "pending",
			Confirmations:   5,
		}
		require.NoError(t, database.Client().Create(oldPending).Error)
		
		// Set old timestamp for pending transaction
		oldTime := time.Now().Add(-25 * time.Hour)
		require.NoError(t, database.Client().Model(oldPending).Update("updated_at", oldTime).Error)

		// Should delete nothing
		deletedCount, err := database.DeleteOldConfirmedTransactions(time.Hour)
		require.NoError(t, err)
		require.Equal(t, int64(0), deletedCount)

		// Verify both transactions still exist
		var count int64
		require.NoError(t, database.Client().Model(&store.GatewayTransaction{}).Count(&count).Error)
		require.Equal(t, int64(2), count)
	})

	t.Run("ZeroRetentionPeriod", func(t *testing.T) {
		// Clean up
		database.Client().Exec("DELETE FROM gateway_transactions")

		// Create a confirmed transaction
		confirmedTx := &store.GatewayTransaction{
			ChainID:         "eip155:1",
			TxHash:          "0x9999",
			BlockNumber:     600,
			Method:          "test",
			EventIdentifier: "zero_retention",
			Status:          "confirmed",
			Confirmations:   15,
		}
		require.NoError(t, database.Client().Create(confirmedTx).Error)

		// With zero retention period, even recent confirmed transactions should be deleted
		deletedCount, err := database.DeleteOldConfirmedTransactions(0)
		require.NoError(t, err)
		require.Equal(t, int64(1), deletedCount)

		// Verify database is empty
		var count int64
		require.NoError(t, database.Client().Model(&store.GatewayTransaction{}).Count(&count).Error)
		require.Equal(t, int64(0), count)
	})

	t.Run("VeryLongRetentionPeriod", func(t *testing.T) {
		// Create an old confirmed transaction
		oldConfirmed := &store.GatewayTransaction{
			ChainID:         "eip155:1",
			TxHash:          "0xAAAA",
			BlockNumber:     700,
			Method:          "test",
			EventIdentifier: "very_old",
			Status:          "confirmed",
			Confirmations:   20,
		}
		require.NoError(t, database.Client().Create(oldConfirmed).Error)
		
		// Set to 1 year ago
		oldTime := time.Now().Add(-365 * 24 * time.Hour)
		require.NoError(t, database.Client().Model(oldConfirmed).Update("updated_at", oldTime).Error)

		// With very long retention period, nothing should be deleted
		deletedCount, err := database.DeleteOldConfirmedTransactions(400 * 24 * time.Hour) // 400 days
		require.NoError(t, err)
		require.Equal(t, int64(0), deletedCount)

		// Verify transaction still exists
		var count int64
		require.NoError(t, database.Client().Model(&store.GatewayTransaction{}).Count(&count).Error)
		require.Equal(t, int64(1), count)
	})
}