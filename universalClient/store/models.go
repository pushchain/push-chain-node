// Package store contains GORM-backed SQLite models used by the Universal Validator.
//
// Database Structure (database file: chain_data.db):
//
//	chains/
//	├── push/
//	│   └── chain_data.db
//	│       ├── chain_states
//	│       └── events
//	└── {external_chain_caip_format}/
//	    └── chain_data.db
//	        ├── chain_states
//	        ├── chain_transactions
//	        └── gas_vote_transactions
package store

import (
	"gorm.io/gorm"
)

// ChainState tracks synchronization state for a chain.
// One record per database (each chain has its own DB).
type ChainState struct {
	gorm.Model
	LastBlock uint64 // Last processed block height
}

// ChainTransaction tracks inbound transaction events from external chains
// (Ethereum, Solana, etc.) that need processing and voting on Push chain.
//
// TODO: Rename to ECEvent (External Chain Event) and update table name to "events"
type ChainTransaction struct {
	gorm.Model
	TxHash           string `gorm:"uniqueIndex:idx_tx_hash_log_index"` // Transaction hash from external chain
	LogIndex         uint   `gorm:"uniqueIndex:idx_tx_hash_log_index"` // Log index within transaction
	BlockNumber      uint64 // Block number (or slot for Solana) on external chain
	EventIdentifier  string // Event type identifier
	Status           string `gorm:"index"` // "confirmation_pending", "awaiting_vote", "confirmed", "failed", "reorged"
	Confirmations    uint64 // Number of block confirmations received
	ConfirmationType string // "STANDARD" or "FAST"
	Data             []byte // Raw JSON-encoded event data
	VoteTxHash       string // Vote transaction hash on Push chain (empty until voted)
}

// GasVoteTransaction tracks gas price votes sent to Push chain for an external chain.
type GasVoteTransaction struct {
	gorm.Model
	GasPrice   uint64 `gorm:"not null"`          // Gas price voted for (wei for EVM chains, lamports for Solana chains)
	VoteTxHash string `gorm:"index"`             // Vote transaction hash on Push chain
	Status     string `gorm:"default:'success'"` // "success" or "failed"
	ErrorMsg   string `gorm:"type:text"`         // Error message if vote failed
}

// PCEvent tracks Push Chain events (TSS protocol events: KeyGen, KeyRefresh, QuorumChange, Sign).
// Table name: "events" (PC_EVENTS)
type PCEvent struct {
	gorm.Model
	EventID           string `gorm:"uniqueIndex;not null"` // Unique event identifier (typically process ID)
	BlockHeight       uint64 `gorm:"index;not null"`       // Block height on Push chain where event was detected
	ExpiryBlockHeight uint64 `gorm:"index;not null"`       // Block height when event expires
	Type              string // "KEYGEN", "KEYREFRESH", "QUORUM_CHANGE", or "SIGN"
	Status            string `gorm:"index;not null"` // "PENDING", "IN_PROGRESS", "BROADCASTED", "COMPLETED", "REVERTED"
	EventData         []byte // Raw JSON-encoded event data from chain
	TxHash            string // Transaction hash on Push chain (empty until voted)
	ErrorMsg          string `gorm:"type:text"` // Error message if processing failed
}

// TableName specifies the table name for PCEvent.
func (PCEvent) TableName() string {
	return "events"
}
