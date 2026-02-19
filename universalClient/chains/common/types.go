package common

import (
	"context"
	"math/big"

	uetypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
)

// ChainClient defines the interface for chain-specific implementations
type ChainClient interface {
	// Start initializes and starts the chain client
	Start(ctx context.Context) error

	// Stop gracefully shuts down the chain client
	Stop() error

	// IsHealthy checks if the chain client is operational
	IsHealthy() bool

	// GetTxBuilder returns the OutboundTxBuilder for this chain
	// Returns an error if txBuilder is not supported for this chain (e.g., Push chain)
	GetTxBuilder() (OutboundTxBuilder, error)
}

// UnSignedOutboundTxReq contains the request for signing an outbound transaction
type UnSignedOutboundTxReq struct {
	SigningHash []byte   // Hash to be signed by TSS
	Nonce       uint64   // evm - TSS Address nonce | svm - PDA nonce
	GasPrice    *big.Int // evm - Gas price used | svm - Prioritization fee
}

// OutboundTxBuilder builds and broadcasts transactions for outbound transfers
type OutboundTxBuilder interface {
	// GetOutboundSigningRequest creates a signing request from outbound event data
	GetOutboundSigningRequest(ctx context.Context, data *uetypes.OutboundCreatedEvent, gasPrice *big.Int, nonce uint64) (*UnSignedOutboundTxReq, error)

	// GetNextNonce returns the next nonce for the given signer on this chain (for seeding local nonce).
	// useFinalized: for EVM, if true use finalized block nonce (aggressive/replace stuck); if false use pending. SVM ignores this.
	GetNextNonce(ctx context.Context, signerAddress string, useFinalized bool) (uint64, error)

	// BroadcastOutboundSigningRequest assembles and broadcasts a signed transaction from the signing request, event data, and signature
	BroadcastOutboundSigningRequest(ctx context.Context, req *UnSignedOutboundTxReq, data *uetypes.OutboundCreatedEvent, signature []byte) (string, error)

	// VerifyBroadcastedTx checks the status of a broadcasted transaction on the destination chain.
	// Returns (found, confirmations, status, error):
	// - found=false: tx not found or not yet mined
	// - found=true: tx exists on-chain
	//   - confirmations: number of blocks since the tx was mined (0 = just mined)
	//   - status: 0 = failed/reverted, 1 = success
	VerifyBroadcastedTx(ctx context.Context, txHash string) (found bool, confirmations uint64, status uint8, err error)
}

// UniversalTx Payload
type UniversalTx struct {
	SourceChain         string                   `json:"sourceChain"`
	LogIndex            uint                     `json:"logIndex"`
	Sender              string                   `json:"sender"`
	Recipient           string                   `json:"recipient"`
	Token               string                   `json:"bridgeToken"`
	Amount              string                   `json:"bridgeAmount"` // uint256 as decimal string
	Payload             uetypes.UniversalPayload `json:"universalPayload"`
	VerificationData    string                   `json:"verificationData"`
	RevertFundRecipient string                   `json:"revertFundRecipient,omitempty"`
	RevertMsg           string                   `json:"revertMsg,omitempty"` // hex-encoded bytes (0x…)
	TxType              uint                     `json:"txType"`              // enum backing uint as decimal string
}

// OutboundEvent represents an outbound observation event from the gateway contract
// Event structure:
// - txID at 1st indexed position (bytes32)
// - universalTxID at 2nd indexed position (bytes32)
type OutboundEvent struct {
	TxID          string `json:"tx_id"`           // bytes32 hex-encoded (0x...)
	UniversalTxID string `json:"universal_tx_id"` // bytes32 hex-encoded (0x...)
}

// Event type enum values for event classification.
// These constants define the types of events that can be processed.
const (
	EventTypeKeygen       = "KEYGEN"
	EventTypeKeyrefresh   = "KEYREFRESH"
	EventTypeQuorumChange = "QUORUM_CHANGE"
	EventTypeSign         = "SIGN"
	EventTypeInbound      = "INBOUND"
	EventTypeOutbound     = "OUTBOUND"
)
