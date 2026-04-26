// Package db provides a lightweight GORM-based SQLite wrapper for persisting
// state required by the Push Universal Validator (UV), such as block tracking,
// processed cross-chain messages, and transaction receipts.
package db

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pushchain/push-chain-node/universalClient/store"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const (
	// InMemorySQLiteDSN is a special DSN to create an ephemeral in-memory SQLite database.
	InMemorySQLiteDSN = ":memory:"

	// dbDirPermissions sets directory permissions to 750 (rwxr-x---).
	dbDirPermissions = 0o750
)

func newGormConfig() *gorm.Config {
	return &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	}
}

func schemaModels() []any {
	return []any{
		&store.State{},
		&store.Event{},
	}
}

// DB wraps a GORM client and provides simplified DB lifecycle management.
type DB struct {
	client *gorm.DB
}

// OpenFileDB opens (or creates) a file-backed SQLite database located in the given directory.
// If `migrateSchema` is true, all defined schema models are automatically migrated.
func OpenFileDB(dir, filename string, migrateSchema bool) (*DB, error) {
	dsn, err := prepareFilePath(dir, filename)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare database path: %w", err)
	}
	return openSQLite(dsn, migrateSchema)
}

// OpenInMemoryDB opens a non-persistent SQLite database in memory.
// This is useful for testing or ephemeral state.
func OpenInMemoryDB(migrateSchema bool) (*DB, error) {
	return openSQLite(InMemorySQLiteDSN, migrateSchema)
}

// openSQLite creates a GORM-backed database instance using the given SQLite DSN.
// If migrateSchema is true, GORM auto-migrates all schema models.
func openSQLite(dsn string, migrateSchema bool) (*DB, error) {
	// Add SQLite connection parameters for concurrent access
	// Only add parameters if it's a file database (not in-memory)
	if dsn != InMemorySQLiteDSN && !strings.Contains(dsn, "?") {
		dsn += "?_journal_mode=WAL&_busy_timeout=5000&cache=shared&mode=rwc"
	}

	db, err := gorm.Open(sqlite.Open(dsn), newGormConfig())
	if err != nil {
		return nil, fmt.Errorf("failed to open SQLite database: %w", err)
	}

	if migrateSchema {
		if err := db.AutoMigrate(schemaModels()...); err != nil {
			return nil, fmt.Errorf("failed to auto-migrate database schema: %w", err)
		}
	}

	// Configure connection pool for better concurrent access
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get underlying sql.DB: %w", err)
	}

	// Configure connection pool based on database type
	if dsn == InMemorySQLiteDSN {
		// In-memory databases should use single connection to maintain state
		// Multiple connections to :memory: create separate databases
		sqlDB.SetMaxOpenConns(1)
		sqlDB.SetMaxIdleConns(1)
	} else {
		// File-based databases can use connection pooling with WAL mode
		sqlDB.SetMaxOpenConns(10)
		sqlDB.SetMaxIdleConns(10)
	}
	// Set maximum lifetime of a connection
	sqlDB.SetConnMaxLifetime(0) // Connections don't expire

	// Apply SQLite performance optimizations for file-based databases
	if err := optimizeSQLiteSettings(db, dsn); err != nil {
		return nil, fmt.Errorf("failed to apply SQLite optimizations: %w", err)
	}

	return &DB{client: db}, nil
}

// optimizeSQLiteSettings applies performance-oriented PRAGMA settings to SQLite
func optimizeSQLiteSettings(db *gorm.DB, dsn string) error {
	// Skip optimizations for in-memory databases as they don't support all PRAGMAs
	if dsn == InMemorySQLiteDSN {
		return nil
	}

	pragmas := []string{
		"PRAGMA synchronous = NORMAL",  // Faster writes, still safe in WAL mode (2-10x faster)
		"PRAGMA cache_size = -64000",   // 64MB in-memory page cache (vs default ~2MB)
		"PRAGMA temp_store = MEMORY",   // Temporary tables and indices stored in RAM
		"PRAGMA mmap_size = 268435456", // 256MB memory-mapped I/O for faster reads
		"PRAGMA foreign_keys = ON",     // Enforce foreign key constraints (data integrity)
	}

	for _, pragma := range pragmas {
		if err := db.Exec(pragma).Error; err != nil {
			return fmt.Errorf("failed to execute %s: %w", pragma, err)
		}
	}

	return nil
}

// Client returns the internal *gorm.DB instance for direct usage in queries.
func (d *DB) Client() *gorm.DB {
	return d.client
}

// Close safely closes the underlying database connection.
func (d *DB) Close() error {
	sqlDB, err := d.client.DB()
	if err != nil {
		return fmt.Errorf("failed to retrieve native sql.DB: %w", err)
	}

	if err := sqlDB.Close(); err != nil {
		return fmt.Errorf("failed to close database connection: %w", err)
	}

	return nil
}

// prepareFilePath ensures the target directory exists and returns the full database file path.
func prepareFilePath(dir, filename string) (string, error) {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		if err := os.MkdirAll(dir, dbDirPermissions); err != nil {
			return "", fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	} else if err != nil {
		return "", fmt.Errorf("error checking directory: %w", err)
	}

	return filepath.Join(dir, filename), nil
}
