// Package store contains GORM-backed SQLite models used by the Universal Validator.
package store

import (
	"gorm.io/gorm"
)

// ChainState tracks the state for the chain this database belongs to.
// Since each chain has its own database, there's only one row per database.
type ChainState struct {
	gorm.Model
	LastBlock int64
	// Can add more chain-specific state fields as needed (e.g., LastSync, Metadata)
}

// ChainTransaction tracks transactions for the chain this database belongs to.
// Since each chain has its own database, ChainID is not needed.
type ChainTransaction struct {
	gorm.Model
	TxHash          string `gorm:"uniqueIndex"`
	BlockNumber     uint64
	Method          string
	EventIdentifier string
	Status          string `gorm:"index"` // "pending", "fast_confirmed", "confirmed", "failed", "reorged"
	Confirmations   uint64
	Data            []byte // Store raw event data
}
