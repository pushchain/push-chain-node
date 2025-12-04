package coordinator

import (
	"context"

	"github.com/pushchain/push-chain-node/x/uvalidator/types"
)

// SendFunc is a function type for sending messages to participants.
// peerID: The peer ID of the recipient
// data: The message bytes
type SendFunc func(ctx context.Context, peerID string, data []byte) error

// DataProvider provides access to Push Chain data.
type DataProvider interface {
	GetLatestBlockNum() (uint64, error)
	GetUniversalValidators() ([]*types.UniversalValidator, error)
	GetCurrentTSSKeyId() (string, error)
}

// ProtocolType enumerates the supported DKLS protocol flows.
type ProtocolType string

const (
	ProtocolKeygen       ProtocolType = "keygen"
	ProtocolKeyrefresh   ProtocolType = "keyrefresh"
	ProtocolQuorumChange ProtocolType = "quorumchange"
	ProtocolSign         ProtocolType = "sign"
)

// Message represents a simple message with type, eventId, payload, and participants.
type Message struct {
	Type         string   `json:"type"` // "setup", "ack", "begin", "step"
	EventID      string   `json:"eventId"`
	Payload      []byte   `json:"payload"`
	Participants []string `json:"participants"` // Array of PartyIDs (validator addresses) participating in this process
}
