package db

import (
	"path/filepath"
	"testing"

	"github.com/pushchain/push-chain-node/universalClient/config"
	"github.com/pushchain/push-chain-node/universalClient/logger"
	"github.com/pushchain/push-chain-node/universalClient/store"
	"github.com/stretchr/testify/require"
)

func TestChainDBManager(t *testing.T) {
	// Setup test config and logger
	cfg := &config.Config{
		LogLevel:  0, // Debug level
		LogFormat: "console",
	}
	log := logger.Init(*cfg)

	t.Run("InMemoryManager", func(t *testing.T) {
		manager := NewInMemoryChainDBManager(log, cfg)
		defer manager.CloseAll()

		// Test getting database for chain
		chainID := "eip155:1"
		db1, err := manager.GetChainDB(chainID)
		require.NoError(t, err)
		require.NotNil(t, db1)

		// Test getting same database again
		db2, err := manager.GetChainDB(chainID)
		require.NoError(t, err)
		require.Equal(t, db1, db2) // Should return same instance

		// Test different chain
		chainID2 := "eip155:137"
		db3, err := manager.GetChainDB(chainID2)
		require.NoError(t, err)
		require.NotNil(t, db3)
		require.NotEqual(t, db1, db3) // Should be different instance

		// Test stats
		stats := manager.GetDatabaseStats()
		require.Equal(t, 2, stats["total_databases"])
		require.Equal(t, true, stats["in_memory"])

		chains, ok := stats["chains"].([]string)
		require.True(t, ok)
		require.Len(t, chains, 2)
		require.Contains(t, chains, chainID)
		require.Contains(t, chains, chainID2)
	})

	t.Run("FileManagerWithTempDir", func(t *testing.T) {
		tempDir := t.TempDir()
		manager := NewChainDBManager(tempDir, log, cfg)
		defer manager.CloseAll()

		// Test getting database for chain
		chainID := "eip155:1"
		db1, err := manager.GetChainDB(chainID)
		require.NoError(t, err)
		require.NotNil(t, db1)

		// Test that database file was created
		expectedPath := filepath.Join(tempDir, "chains", "eip155_1", "chain_data.db")
		require.FileExists(t, expectedPath)

		// Test special characters in chain ID
		chainID2 := "solana:mainnet-beta"
		db2, err := manager.GetChainDB(chainID2)
		require.NoError(t, err)
		require.NotNil(t, db2)

		// Test stats
		stats := manager.GetDatabaseStats()
		require.Equal(t, 2, stats["total_databases"])
		require.Equal(t, false, stats["in_memory"])
		require.Equal(t, tempDir, stats["base_directory"])
	})

	t.Run("CloseSpecificDatabase", func(t *testing.T) {
		manager := NewInMemoryChainDBManager(log, cfg)
		defer manager.CloseAll()

		chainID := "eip155:1"
		db, err := manager.GetChainDB(chainID)
		require.NoError(t, err)
		require.NotNil(t, db)

		// Close specific database
		err = manager.CloseChainDB(chainID)
		require.NoError(t, err)

		// Stats should show no databases
		stats := manager.GetDatabaseStats()
		require.Equal(t, 0, stats["total_databases"])
	})

	t.Run("DatabaseOperations", func(t *testing.T) {
		manager := NewInMemoryChainDBManager(log, cfg)
		defer manager.CloseAll()

		chainID := "eip155:1"
		db, err := manager.GetChainDB(chainID)
		require.NoError(t, err)

		// Test basic database operations
		tx := &store.ChainTransaction{
			TxHash:          "0x123",
			BlockNumber:     1000,
			EventIdentifier: "event1",
			Status:          "pending",
			Confirmations:   0,
		}

		// Create transaction
		err = db.Client().Create(tx).Error
		require.NoError(t, err)

		// Query transaction
		var retrieved store.ChainTransaction
		err = db.Client().Where("tx_hash = ?", "0x123").First(&retrieved).Error
		require.NoError(t, err)
		require.Equal(t, "0x123", retrieved.TxHash)
	})

	t.Run("ChainIDSanitization", func(t *testing.T) {
		testCases := []struct {
			input    string
			expected string
		}{
			{"eip155:1", "eip155_1"},
			{"solana:mainnet-beta", "solana_mainnet-beta"},
			{"cosmos:cosmoshub-4", "cosmos_cosmoshub-4"},
			{"special:chars@#$", "special_chars___"},
		}

		for _, tc := range testCases {
			result := sanitizeChainID(tc.input)
			require.Equal(t, tc.expected, result, "Input: %s", tc.input)
		}
	})
}

func TestChainDBManagerConcurrency(t *testing.T) {
	cfg := &config.Config{
		LogLevel:  0,
		LogFormat: "console",
	}
	log := logger.Init(*cfg)

	manager := NewInMemoryChainDBManager(log, cfg)
	defer manager.CloseAll()

	// Test concurrent access to same chain
	chainID := "eip155:1"
	
	// Pre-initialize the database to ensure schema is migrated before concurrent access
	initDB, err := manager.GetChainDB(chainID)
	require.NoError(t, err)
	require.NotNil(t, initDB)
	
	done := make(chan bool, 10)

	for i := 0; i < 10; i++ {
		go func(id int) {
			defer func() { done <- true }()
			
			db, err := manager.GetChainDB(chainID)
			require.NoError(t, err)
			require.NotNil(t, db)

			// Perform some database operation
			tx := &store.ChainTransaction{
				TxHash:          string(rune('a'+id)) + "123",
				BlockNumber:     uint64(1000 + id),
				EventIdentifier: "event1",
				Status:          "pending",
				Confirmations:   0,
			}

			err = db.Client().Create(tx).Error
			require.NoError(t, err)
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Verify all transactions were created
	db, err := manager.GetChainDB(chainID)
	require.NoError(t, err)

	var count int64
	err = db.Client().Model(&store.ChainTransaction{}).Count(&count).Error
	require.NoError(t, err)
	require.Equal(t, int64(10), count)

	// Stats should show only one database
	stats := manager.GetDatabaseStats()
	require.Equal(t, 1, stats["total_databases"])
}