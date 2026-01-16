// Package store contains GORM-backed SQLite models used by the Universal Validator.
package store

import (
	"gorm.io/gorm"
)

// Database Structure:
//
//	{CHAIN_CAIP2_FORMAT}.db (e.g., "eip155:1.db")
//	├── states
//	└── events

// State tracks synchronization state for a chain.
// There is exactly one State record per chain database, storing the last processed block height.
type State struct {
	gorm.Model
	BlockHeight uint64 // Last processed block height (or slot for Solana chains)
}

// Event tracks events for a chain.
type Event struct {
	gorm.Model

	// For PC Outbound events: this is the TxID or ProcessId.
	// For external chains: this is composed of TxHash + LogIndex.
	EventID string `gorm:"uniqueIndex;not null"`

	// BlockHeight (or slot for Solana) where the event was observed.
	BlockHeight uint64 `gorm:"index;not null"`

	// ExpiryBlockHeight is the block height when the event expires
	// For certain events, this is kept quite high to avoid premature expiration.
	ExpiryBlockHeight uint64 `gorm:"index"`

	// For PC: "KEYGEN", "KEYREFRESH", "QUORUM_CHANGE", "SIGN"
	// For external chains: "INBOUND" "OUTBOUND"
	Type string `gorm:"index;not null"`

	// ConfirmationType: "STANDARD", "FAST", "INSTANT"
	// Depends on the finality of a chain
	ConfirmationType string `gorm:"index;not null"`

	// Status tracks the processing state of the event.
	// For PC: "PENDING", "IN_PROGRESS", "BROADCASTED", "COMPLETED", "REVERTED", "EXPIRED"
	// For external chains: "PENDING", "COMPLETED", "EXPIRED"
	Status string `gorm:"index;not null"`

	// EventData contains the raw JSON-encoded event payload.
	EventData []byte

	// VoteTxHash is the transaction hash of the vote on the Push chain
	VoteTxHash string `gorm:"default:NULL"`

	// BroadcastedTxHash is the broadcasted txHash - only for "SIGN" PC events
	BroadcastedTxHash string `gorm:"default:NULL"`
}
