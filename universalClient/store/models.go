// Package store contains GORM-backed SQLite models used by the Universal Validator.
package store

import (
	"gorm.io/gorm"
)

// ChainState tracks the state for the chain this database belongs to.
// Since each chain has its own database, there's only one row per database.
type ChainState struct {
	gorm.Model
	LastBlock uint64
	// Can add more chain-specific state fields as needed (e.g., LastSync, Metadata)
}

// ChainTransaction tracks transactions for the chain this database belongs to.
// Since each chain has its own database, ChainID is not needed.
type ChainTransaction struct {
	gorm.Model
	TxHash           string `gorm:"uniqueIndex:idx_tx_hash_log_index"`
	LogIndex         uint   `gorm:"uniqueIndex:idx_tx_hash_log_index"`
	BlockNumber      uint64
	EventIdentifier  string
	Status           string `gorm:"index"` // "confirmation_pending", "awaiting_vote", "confirmed", "failed", "reorged"
	Confirmations    uint64
	ConfirmationType string // "STANDARD" or "FAST" - which confirmation type this tx requires
	Data             []byte // Store raw event data
	VoteTxHash       string // Transaction hash of the vote on pchain
}

// GasVoteTransaction tracks gas price votes for the chain this database belongs to.
// Since each chain has its own database, ChainID is not needed.
// Uses GORM's built-in CreatedAt/UpdatedAt for timestamp tracking.
type GasVoteTransaction struct {
	gorm.Model
	GasPrice   uint64 `gorm:"not null"` // Gas price voted for (in wei)
	VoteTxHash string `gorm:"index"`    // On-chain vote transaction hash
	Status     string `gorm:"default:'success'"`
	ErrorMsg   string `gorm:"type:text"` // Error message if vote failed
}

// TSSEvent tracks TSS protocol events (KeyGen, KeyRefresh, Sign) from Push Chain.
type TSSEvent struct {
	gorm.Model
	EventID      string `gorm:"uniqueIndex;not null"` // Unique identifier for the event
	BlockNumber  uint64 `gorm:"index;not null"`       // Block number when event was detected
	ProtocolType string // "keygen", "keyrefresh", or "sign"
	Status       string `gorm:"index;not null"` // "PENDING", "IN_PROGRESS", "SUCCESS", "FAILED", "EXPIRED"
	ExpiryHeight uint64 `gorm:"index;not null"` // Block height when event expires
	EventData    []byte // Raw event data from chain
	VoteTxHash   string // Transaction hash of the vote on pchain
	ErrorMsg     string `gorm:"type:text"` // Error message if status is FAILED
}

// ExternalChainSignature tracks signatures that need to be broadcasted to external chains.
// Created when a Sign protocol completes successfully.
// TODO: Finalize Structure
type ChainTSSTransaction struct {
	gorm.Model
	TSSEventID      uint   `gorm:"index;not null"` // Reference to TSSEvent
	Status          string `gorm:"index;not null"` // "PENDING" or "SUCCESS" (after broadcast)
	Signature       []byte `gorm:"not null"`       // ECDSA signature (65 bytes: R(32) + S(32) + RecoveryID(1))
	MessageHash     []byte `gorm:"not null"`       // Message hash that was signed
	BroadcastTxHash string `gorm:"index"`          // Transaction hash after successful broadcast
	ErrorMsg        string `gorm:"type:text"`      // Error message if broadcast failed
}
