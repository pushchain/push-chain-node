package db

import (
	"path/filepath"
	"testing"

	"github.com/pushchain/push-chain-node/universalClient/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDB_OpenModes(t *testing.T) {
	t.Run("in-memory alias", func(t *testing.T) {
		db, err := OpenInMemoryDB(true)
		require.NoError(t, err)
		require.NotNil(t, db)

		runSampleInsertSelectTest(t, db)
		assert.NoError(t, db.Close())
	})

	t.Run("in-memory direct", func(t *testing.T) {
		db, err := openSQLite(InMemorySQLiteDSN, true)
		require.NoError(t, err)
		require.NotNil(t, db)

		runSampleInsertSelectTest(t, db)
		assert.NoError(t, db.Close())
	})

	t.Run("file-based DB", func(t *testing.T) {
		dir := t.TempDir()
		dbName := "test.db"

		db, err := OpenFileDB(dir, dbName, true)
		require.NoError(t, err)
		require.NotNil(t, db)

		assert.FileExists(t, filepath.Join(dir, dbName))

		runSampleInsertSelectTest(t, db)

		assert.NoError(t, db.Close())

		t.Run("close twice", func(t *testing.T) {
			assert.NoError(t, db.Close())
		})
	})

	t.Run("invalid path fails", func(t *testing.T) {
		db, err := OpenFileDB("///invalid", "db.db", true)
		require.ErrorContains(t, err, "failed to prepare database path")
		require.Nil(t, db)
	})
}

func runSampleInsertSelectTest(t *testing.T, db *DB) {
	// Given a sample row
	entry := store.LastObservedBlock{
		ChainID: "testnet",
		Block:   10101,
	}

	// ACT: Insert
	err := db.Client().Create(&entry).Error
	require.NoError(t, err)

	// ACT: Select
	var result store.LastObservedBlock
	err = db.Client().Where("chain_id = ?", "testnet").First(&result).Error
	require.NoError(t, err)
	assert.Equal(t, int64(10101), result.Block)
}
