// Package store contains data models and enum constants for the Universal Validator.
// All event status, type, and confirmation type constants are defined here
// as the single source of truth — import from here, not from individual packages.
package store

import (
	"gorm.io/gorm"
)

// Event status values.
const (
	StatusPending     = "PENDING"     // Observed on external chain, awaiting confirmations
	StatusConfirmed   = "CONFIRMED"   // Confirmed (ready for processing or voting)
	StatusInProgress  = "IN_PROGRESS" // TSS signing in progress
	StatusSigned      = "SIGNED"      // TSS signing done, tx not yet broadcast
	StatusBroadcasted = "BROADCASTED" // Transaction sent to external chain
	StatusCompleted   = "COMPLETED"   // Successfully completed
	StatusReverted    = "REVERTED"    // Failed (expiry, receipt failed, or vote failed)
	StatusReorged     = "REORGED"     // Removed due to chain reorganization
)

// Event type values.
const (
	EventTypeKeygen       = "KEYGEN"
	EventTypeKeyrefresh   = "KEYREFRESH"
	EventTypeQuorumChange = "QUORUM_CHANGE"
	EventTypeSignOutbound     = "SIGN_OUTBOUND"
	EventTypeSignFundMigrate  = "SIGN_FUND_MIGRATE"
	EventTypeInbound      = "INBOUND"
	EventTypeOutbound     = "OUTBOUND"
)

// Confirmation type values.
const (
	ConfirmationStandard = "STANDARD" // Standard finality (multiple block confirmations)
	ConfirmationFast     = "FAST"     // Fast finality (fewer confirmations)
	ConfirmationInstant  = "INSTANT"  // Instant finality (Push Chain)
)

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
	// For PC: (instant finality) "CONFIRMED", "IN_PROGRESS", "BROADCASTED" -> (terminal states) "REVERTED", "COMPLETED"
	// For external chains: "PENDING", "CONFIRMED", -> (terminal states) "REORGED", "COMPLETED"
	Status string `gorm:"index;not null"`

	// EventData contains the raw JSON-encoded event payload.
	EventData []byte

	// VoteTxHash is the transaction hash of the vote on the Push chain
	VoteTxHash string `gorm:"default:NULL"`

	// BroadcastedTxHash is the broadcasted txHash - only for "SIGN" PC events
	BroadcastedTxHash string `gorm:"default:NULL"`
}
