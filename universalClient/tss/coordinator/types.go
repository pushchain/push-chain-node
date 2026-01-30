package coordinator

import (
	"context"

	"github.com/pushchain/push-chain-node/universalClient/chains/common"
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

// Message represents a simple message with type, eventId, payload, and participants.
type Message struct {
	Type         string   `json:"type"` // "setup", "ack", "begin", "step"
	EventID      string   `json:"eventId"`
	Payload      []byte   `json:"payload"`
	Participants []string `json:"participants"` // Array of PartyIDs (validator addresses) participating in this process

	// UnSignedOutboundTxReq is included for SIGN protocol setup messages.
	// Participants use this to verify the signing hash before proceeding.
	UnSignedOutboundTxReq *common.UnSignedOutboundTxReq `json:"unsigned_outbound_tx_req,omitempty"`
}
