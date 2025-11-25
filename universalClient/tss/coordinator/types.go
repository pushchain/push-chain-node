package coordinator

import (
	"context"
)

// SendFunc is a function type for sending messages to participants.
// peerID: The peer ID of the recipient
// data: The message bytes
type SendFunc func(ctx context.Context, peerID string, data []byte) error

// DataProvider provides access to Push Chain data.
type DataProvider interface {
	GetLatestBlockNum(ctx context.Context) (uint64, error)
	GetUniversalValidators(ctx context.Context) ([]*UniversalValidator, error)
	GetCurrentTSSKeyId(ctx context.Context) (string, error)
}

// UVStatus represents Universal Validator status.
type UVStatus string

const (
	UVStatusUnspecified  UVStatus = "UV_STATUS_UNSPECIFIED"
	UVStatusActive       UVStatus = "UV_STATUS_ACTIVE"
	UVStatusPendingJoin  UVStatus = "UV_STATUS_PENDING_JOIN"
	UVStatusPendingLeave UVStatus = "UV_STATUS_PENDING_LEAVE"
	UVStatusInactive     UVStatus = "UV_STATUS_INACTIVE"
)

// NetworkInfo contains network metadata.
type NetworkInfo struct {
	PeerID     string   `json:"peer_id"`
	Multiaddrs []string `json:"multiaddrs"`
}

// UniversalValidator represents a universal validator.
type UniversalValidator struct {
	ValidatorAddress string      `json:"validator_address"` // Core validator address (NodeID)
	Status           UVStatus    `json:"status"`            // Current lifecycle status
	Network          NetworkInfo `json:"network"`           // Metadata for networking
	JoinedAtBlock    int64       `json:"joined_at_block"`   // Block height when added to UV set
}

// ProtocolType enumerates the supported DKLS protocol flows.
type ProtocolType string

const (
	ProtocolKeygen     ProtocolType = "keygen"
	ProtocolKeyrefresh ProtocolType = "keyrefresh"
	ProtocolSign       ProtocolType = "sign"
)

// Message represents a simple message with type, eventId, payload, and participants.
type Message struct {
	Type         string   `json:"type"` // "setup", "ack", "begin", "step"
	EventID      string   `json:"eventId"`
	Payload      []byte   `json:"payload"`
	Participants []string `json:"participants"` // Array of PartyIDs (validator addresses) participating in this process
}
