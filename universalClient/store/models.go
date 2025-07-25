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
