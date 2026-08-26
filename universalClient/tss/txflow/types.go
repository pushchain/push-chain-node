// Package txflow holds the shared types and helpers used by both the
// transaction broadcaster and the resolver. Each module owns its own
// lifecycle (broadcaster pushes SIGNED→BROADCASTED, resolver pulls
// BROADCASTED→terminal), but they read the same persisted event payloads
// and apply the same rules (signed-vs-finalized nonce comparison, signing
// data decoding). Lifting those shared concerns here gives one source of
// truth without conflating the two modules' responsibilities.
package txflow

import (
	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
	utsstypes "github.com/pushchain/push-chain-node/x/utss/types"
)

// SigningData holds the signing parameters persisted by sessionManager when
// marking an event SIGNED. Both broadcaster and resolver read these fields
// — broadcaster to assemble + send the tx, resolver to compare the signed
// nonce against the chain's finalized nonce.
type SigningData struct {
	Signature   string `json:"signature"`    // hex-encoded 64/65 byte signature
	SigningHash string `json:"signing_hash"` // hex-encoded signing hash
	Nonce       uint64 `json:"nonce"`
}

// SignedOutboundData wraps OutboundCreatedEvent with the signing data the
// broadcaster needs to assemble the destination-chain tx.
type SignedOutboundData struct {
	uexecutortypes.OutboundCreatedEvent
	SigningData *SigningData `json:"signing_data,omitempty"`
}

// SignedFundMigrationData wraps FundMigrationInitiatedEventData with the
// signing data needed for the migration sweep tx.
type SignedFundMigrationData struct {
	utsstypes.FundMigrationInitiatedEventData
	SigningData *SigningData `json:"signing_data,omitempty"`
}
