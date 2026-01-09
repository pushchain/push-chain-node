package coordinator

import (
	"context"
	"math/big"
)

// SendFunc is a function type for sending messages to participants.
// peerID: The peer ID of the recipient
// data: The message bytes
type SendFunc func(ctx context.Context, peerID string, data []byte) error

// ProtocolType enumerates the supported DKLS protocol flows.
type ProtocolType string

const (
	ProtocolKeygen       ProtocolType = "KEYGEN"
	ProtocolKeyrefresh   ProtocolType = "KEYREFRESH"
	ProtocolQuorumChange ProtocolType = "QUORUM_CHANGE"
	ProtocolSign         ProtocolType = "SIGN"
)

// SignMetadata contains the signing parameters from the coordinator.
// Participants independently build the transaction using these parameters
// and verify the resulting hash matches before signing.
type SignMetadata struct {
	// GasPrice is the gas price chosen by coordinator from the on-chain oracle.
	GasPrice *big.Int `json:"gas_price"`

	// SigningHash is the hash computed by the coordinator.
	// Participants verify this matches their independently computed hash.
	SigningHash []byte `json:"signing_hash"`
}

// Message represents a simple message with type, eventId, payload, and participants.
type Message struct {
	Type         string   `json:"type"` // "setup", "ack", "begin", "step"
	EventID      string   `json:"eventId"`
	Payload      []byte   `json:"payload"`
	Participants []string `json:"participants"` // Array of PartyIDs (validator addresses) participating in this process

	// SignMetadata is included for SIGN protocol setup messages.
	// Participants use this to verify the signing hash before proceeding.
	SignMetadata *SignMetadata `json:"sign_metadata,omitempty"`
}
