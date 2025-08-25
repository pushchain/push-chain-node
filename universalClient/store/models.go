// Package store contains GORM-backed SQLite models used by the Universal Validator.
package store

import (
	"gorm.io/gorm"
)

// LastObservedBlock tracks the latest block observed for a given chain.
type LastObservedBlock struct {
	gorm.Model
	ChainID string `gorm:"uniqueIndex"`
	Block   int64
}

// GatewayTransaction tracks gateway transactions and their confirmation status
type GatewayTransaction struct {
	gorm.Model
	ChainID         string `gorm:"index"`
	TxHash          string `gorm:"uniqueIndex"`
	BlockNumber     uint64
	Method          string
	EventIdentifier string
	Status          string `gorm:"index"` // "pending", "fast_confirmed", "confirmed", "failed", "reorged"
	Confirmations   uint64
	Data            []byte // Store raw event data
}
