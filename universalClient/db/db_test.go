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
	})

	t.Run("file-based DB creates directory", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "nested", "dir")
		dbName := "test.db"

		db, err := OpenFileDB(dir, dbName, true)
		require.NoError(t, err)
		require.NotNil(t, db)

		assert.FileExists(t, filepath.Join(dir, dbName))
		assert.NoError(t, db.Close())
	})

	t.Run("invalid path fails", func(t *testing.T) {
		db, err := OpenFileDB("///invalid", "db.db", true)
		require.ErrorContains(t, err, "failed to prepare database path")
		require.Nil(t, db)
	})
}

func TestDB_PragmaOptimizations(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenFileDB(dir, "pragma_test.db", true)
	require.NoError(t, err)
	defer db.Close()

	// Verify WAL mode is active
	var journalMode string
	err = db.Client().Raw("PRAGMA journal_mode").Scan(&journalMode).Error
	require.NoError(t, err)
	assert.Equal(t, "wal", journalMode)

	// Verify synchronous is NORMAL (1)
	var syncMode int
	err = db.Client().Raw("PRAGMA synchronous").Scan(&syncMode).Error
	require.NoError(t, err)
	assert.Equal(t, 1, syncMode)

	// Verify foreign keys are enabled
	var fkEnabled int
	err = db.Client().Raw("PRAGMA foreign_keys").Scan(&fkEnabled).Error
	require.NoError(t, err)
	assert.Equal(t, 1, fkEnabled)
}

func TestDB_OpenWithoutMigration(t *testing.T) {
	db, err := OpenInMemoryDB(false)
	require.NoError(t, err)
	require.NotNil(t, db)
	assert.NoError(t, db.Close())
}

func TestDB_FileDBExistingDirectory(t *testing.T) {
	dir := t.TempDir()
	// Open twice — second time directory already exists
	db1, err := OpenFileDB(dir, "test1.db", true)
	require.NoError(t, err)
	db1.Close()

	db2, err := OpenFileDB(dir, "test2.db", true)
	require.NoError(t, err)
	db2.Close()
}

func TestDB_ClientReturnsGorm(t *testing.T) {
	db, err := OpenInMemoryDB(true)
	require.NoError(t, err)
	defer db.Close()

	assert.NotNil(t, db.Client())
}

func TestDB_SchemaModels(t *testing.T) {
	models := schemaModels()
	assert.Len(t, models, 2)
}

func runSampleInsertSelectTest(t *testing.T, db *DB) {
	// Given a sample row
	entry := store.State{
		BlockHeight: 10101,
	}

	// ACT: Insert
	err := db.Client().Create(&entry).Error
	require.NoError(t, err)

	// ACT: Select
	var result store.State
	err = db.Client().First(&result).Error
	require.NoError(t, err)
	assert.Equal(t, uint64(10101), result.BlockHeight)
}
