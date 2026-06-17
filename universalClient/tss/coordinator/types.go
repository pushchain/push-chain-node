package coordinator

import (
	"context"
	"math/big"

	"github.com/pushchain/push-chain-node/universalClient/chains/common"
)

// SendFunc sends `data` to `peerID` over the p2p network.
type SendFunc func(ctx context.Context, peerID string, data []byte) error

// MessageType discriminates inter-node TSS coordination messages.
type MessageType string

const (
	MessageTypeSetup              MessageType = "setup"               // coordinator → participants: start a session
	MessageTypeACK                MessageType = "ack"                 // participant → coordinator: ready (or SignedData attached → already signed)
	MessageTypeBegin              MessageType = "begin"               // coordinator → participants: all ACKed, run
	MessageTypeStep               MessageType = "step"                // participant ↔ participant: DKLS protocol round
	MessageTypeSignatureBroadcast MessageType = "signature_broadcast" // participant → all UVs: signature ready, persist & participate in voting
)

// SignedDataPayload is an already-produced signature. Attached to an ACK
// when the participant already holds a valid signature for this event,
// letting the coordinator skip a fresh DKLS run.
type SignedDataPayload struct {
	Signature              []byte   `json:"signature"`              // ECDSA (r || s [|| v])
	SigningHash            []byte   `json:"signing_hash"`           // 32-byte message hash
	Nonce                  uint64   `json:"nonce"`                  // EVM nonce; ignored by SVM
	TSSFundMigrationAmount *big.Int `json:"tss_fund_migration_amount,omitempty"`
}

// Message is the wire format for all TSS coordination messages.
type Message struct {
	Type         MessageType `json:"type"`
	EventID      string      `json:"eventId"`
	Payload      []byte      `json:"payload"`
	Participants []string    `json:"participants"` // PartyIDs (validator addresses)

	// UnsignedSigningReq is set on SIGN setup messages so participants can
	// verify the signing hash independently.
	UnsignedSigningReq *common.UnsignedSigningReq `json:"unsigned_outbound_tx_req,omitempty"`

	// SignedData is set on an ACK to report a prior signature for this event.
	SignedData *SignedDataPayload `json:"signed_data,omitempty"`
}
