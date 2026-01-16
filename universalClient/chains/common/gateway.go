package common

import (
	"context"
	"math/big"

	uetypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
)

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

// OutboundTxResult contains the result of building an outbound transaction
type OutboundTxResult struct {
	SigningHash []byte   // Hash to be signed by TSS
	Nonce       uint64   // Transaction nonce
	GasPrice    *big.Int // Gas price used
	GasLimit    uint64   // Gas limit
	ChainID     string   // Destination chain ID (CAIP-2 format)
	RawTx       []byte   // Raw unsigned transaction bytes
}

// OutboundTxBuilder builds and broadcasts transactions for outbound transfers
type OutboundTxBuilder interface {
	// BuildTransaction builds an unsigned transaction from outbound event data
	BuildTransaction(ctx context.Context, data *uetypes.OutboundCreatedEvent, gasPrice *big.Int) (*OutboundTxResult, error)

	// AssembleSignedTransaction assembles a signed transaction from raw tx and signature
	AssembleSignedTransaction(unsignedTx []byte, signature []byte, recoveryID byte) ([]byte, error)

	// BroadcastTransaction broadcasts a signed transaction and returns the tx hash
	BroadcastTransaction(ctx context.Context, signedTx []byte) (string, error)

	// GetTxHash calculates the transaction hash from signed transaction bytes
	GetTxHash(signedTx []byte) (string, error)

	// GetChainID returns the chain ID this builder is for
	GetChainID() string
}
