package types

import (
	"time"
)

// VerifiedTransaction represents a transaction that has been verified
type VerifiedTransaction struct {
	TxHash      string    // Transaction hash
	ChainId     string    // Chain ID where the transaction occurred
	CaipAddress string    // CAIP address involved in the transaction
	VerifiedAt  time.Time // When the transaction was verified
}
