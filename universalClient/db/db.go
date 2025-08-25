// Package db provides a lightweight GORM-based SQLite wrapper for persisting
// state required by the Push Universal Validator (UV), such as block tracking,
// processed cross-chain messages, and transaction receipts.
package db

import (
	"fmt"
	"os"
	"strings"

	"github.com/pkg/errors"
	"github.com/rollchains/pchain/universalClient/store"
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

var (
	// gormConfig disables logging output for cleaner usage in validator processes.
	gormConfig = &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	}

	// schemaModels lists the structs to be auto-migrated into the database.
	schemaModels = []any{
		&store.LastObservedBlock{},
		&store.GatewayTransaction{},
		// Add additional models here as needed.
	}
)

// DB wraps a GORM client and provides simplified DB lifecycle management.
type DB struct {
	client *gorm.DB
}

// OpenFileDB opens (or creates) a file-backed SQLite database located in the given directory.
// If `migrateSchema` is true, all defined schema models are automatically migrated.
func OpenFileDB(dir, filename string, migrateSchema bool) (*DB, error) {
	dsn, err := prepareFilePath(dir, filename)
	if err != nil {
		return nil, errors.Wrap(err, "failed to prepare database path")
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
	
	db, err := gorm.Open(sqlite.Open(dsn), gormConfig)
	if err != nil {
		return nil, errors.Wrap(err, "failed to open SQLite database")
	}

	if migrateSchema {
		if err := db.AutoMigrate(schemaModels...); err != nil {
			return nil, errors.Wrap(err, "failed to auto-migrate database schema")
		}
	}

	// Configure connection pool for better concurrent access
	sqlDB, err := db.DB()
	if err != nil {
		return nil, errors.Wrap(err, "failed to get underlying sql.DB")
	}
	
	// Set maximum number of open connections
	sqlDB.SetMaxOpenConns(1)  // SQLite performs best with a single connection in WAL mode
	// Set maximum number of idle connections
	sqlDB.SetMaxIdleConns(1)
	// Set maximum lifetime of a connection
	sqlDB.SetConnMaxLifetime(0) // Connections don't expire

	return &DB{client: db}, nil
}

// Client returns the internal *gorm.DB instance for direct usage in queries.
func (d *DB) Client() *gorm.DB {
	return d.client
}

// Close safely closes the underlying database connection.
func (d *DB) Close() error {
	sqlDB, err := d.client.DB()
	if err != nil {
		return errors.Wrap(err, "failed to retrieve native sql.DB")
	}

	if err := sqlDB.Close(); err != nil {
		return errors.Wrap(err, "failed to close database connection")
	}

	return nil
}

// prepareFilePath ensures the target directory exists and returns the full database file path.
// If the directory contains the in-memory DSN string, it is returned as-is.
func prepareFilePath(dir, filename string) (string, error) {
	if strings.Contains(dir, InMemorySQLiteDSN) {
		return dir, nil
	}

	// Ensure the directory exists
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		if err := os.MkdirAll(dir, dbDirPermissions); err != nil {
			return "", errors.Wrapf(err, "failed to create directory: %s", dir)
		}
	} else if err != nil {
		return "", errors.Wrap(err, "error checking directory")
	}

	return fmt.Sprintf("%s/%s", dir, filename), nil
}
