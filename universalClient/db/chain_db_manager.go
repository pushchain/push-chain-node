// Package db provides a lightweight GORM-based SQLite wrapper for persisting
// state required by the Push Universal Validator (UV), with per-chain database isolation.
package db

import (
	"path/filepath"
	"sync"

	"github.com/pkg/errors"
	"github.com/rollchains/pchain/universalClient/config"
	"github.com/rs/zerolog"
)

// ChainDBManager manages per-chain database instances for traffic isolation
type ChainDBManager struct {
	baseDir      string
	databases    map[string]*DB // chainID -> DB instance
	mu           sync.RWMutex
	logger       zerolog.Logger
	inMemory     bool // For testing with in-memory databases
	appConfig    *config.Config
}

// NewChainDBManager creates a new manager for per-chain databases
func NewChainDBManager(baseDir string, logger zerolog.Logger, cfg *config.Config) *ChainDBManager {
	return &ChainDBManager{
		baseDir:   baseDir,
		databases: make(map[string]*DB),
		logger:    logger.With().Str("component", "chain_db_manager").Logger(),
		appConfig: cfg,
	}
}

// NewInMemoryChainDBManager creates a manager with in-memory databases (for testing)
func NewInMemoryChainDBManager(logger zerolog.Logger, cfg *config.Config) *ChainDBManager {
	return &ChainDBManager{
		databases: make(map[string]*DB),
		logger:    logger.With().Str("component", "chain_db_manager").Logger(),
		inMemory:  true,
		appConfig: cfg,
	}
}

// GetChainDB returns a database instance for a specific chain
// Creates the database lazily if it doesn't exist
func (m *ChainDBManager) GetChainDB(chainID string) (*DB, error) {
	// Check if database already exists
	m.mu.RLock()
	if db, exists := m.databases[chainID]; exists {
		m.mu.RUnlock()
		return db, nil
	}
	m.mu.RUnlock()

	// Need to create new database
	m.mu.Lock()
	defer m.mu.Unlock()

	// Double-check after acquiring write lock
	if db, exists := m.databases[chainID]; exists {
		return db, nil
	}

	// Create new database for this chain
	var db *DB
	var err error

	if m.inMemory {
		// For testing - create in-memory database
		db, err = OpenInMemoryDB(true)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to create in-memory database for chain %s", chainID)
		}
		m.logger.Debug().
			Str("chain_id", chainID).
			Msg("created in-memory database for chain")
	} else {
		// Create chain-specific directory and database file
		chainDir := filepath.Join(m.baseDir, "chains", sanitizeChainID(chainID))
		dbFilename := "chain_data.db"
		
		db, err = OpenFileDB(chainDir, dbFilename, true)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to create database for chain %s", chainID)
		}
		
		m.logger.Info().
			Str("chain_id", chainID).
			Str("db_path", filepath.Join(chainDir, dbFilename)).
			Msg("created file database for chain")
	}

	// Store in map
	m.databases[chainID] = db
	
	return db, nil
}

// GetAllDatabases returns all active database instances
func (m *ChainDBManager) GetAllDatabases() map[string]*DB {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	// Return a copy to prevent external modification
	result := make(map[string]*DB)
	for k, v := range m.databases {
		result[k] = v
	}
	return result
}

// CloseChainDB closes and removes a specific chain's database from the manager
func (m *ChainDBManager) CloseChainDB(chainID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	db, exists := m.databases[chainID]
	if !exists {
		return nil // Already closed or never opened
	}
	
	if err := db.Close(); err != nil {
		return errors.Wrapf(err, "failed to close database for chain %s", chainID)
	}
	
	delete(m.databases, chainID)
	m.logger.Info().
		Str("chain_id", chainID).
		Msg("closed database for chain")
	
	return nil
}

// CloseAll closes all database connections
func (m *ChainDBManager) CloseAll() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	var errs []error
	for chainID, db := range m.databases {
		if err := db.Close(); err != nil {
			errs = append(errs, errors.Wrapf(err, "failed to close database for chain %s", chainID))
		}
	}
	
	// Clear the map
	m.databases = make(map[string]*DB)
	
	if len(errs) > 0 {
		return errors.Errorf("failed to close %d databases", len(errs))
	}
	
	return nil
}

// GetDatabaseStats returns statistics about managed databases
func (m *ChainDBManager) GetDatabaseStats() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	chains := make([]string, 0, len(m.databases))
	for chainID := range m.databases {
		chains = append(chains, chainID)
	}
	
	return map[string]interface{}{
		"total_databases": len(m.databases),
		"chains":         chains,
		"in_memory":      m.inMemory,
		"base_directory": m.baseDir,
	}
}

// sanitizeChainID converts chain ID to filesystem-safe format
// e.g., "eip155:1" -> "eip155_1"
func sanitizeChainID(chainID string) string {
	// Replace colons and other special characters with underscores
	result := ""
	for _, r := range chainID {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			result += string(r)
		} else {
			result += "_"
		}
	}
	return result
}