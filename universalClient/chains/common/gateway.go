package common

import (
	"context"

	uetypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
)

// GatewayEvent represents a cross-chain gateway event
type GatewayEvent struct {
	ChainID          string
	TxHash           string
	BlockNumber      uint64
	EventID          string
	Payload          []byte
	Confirmations    uint64
	ConfirmationType string // "STANDARD" or "FAST" - from gateway method config
}

type TxWithFundsPayload struct {
	SourceChain         string                   `json:"sourceChain"`
	LogIndex            uint                     `json:"logIndex"`
	Sender              string                   `json:"sender"`
	Recipient           string                   `json:"recipient"`
	BridgeToken         string                   `json:"bridgeToken"`
	BridgeAmount        string                   `json:"bridgeAmount"` // uint256 as decimal string
	GasAmount           string                   `json:"gasAmount"`    // uint256 as decimal string
	UniversalPayload    uetypes.UniversalPayload `json:"universalPayload"`
	VerificationData    string                   `json:"verificationData"`
	RevertFundRecipient string                   `json:"revertFundRecipient,omitempty"`
	RevertMsg           string                   `json:"revertMsg,omitempty"` // hex-encoded bytes (0x…)
	TxType              uint                     `json:"txType"`              // enum backing uint as decimal string
}

// GatewayOperations defines gateway-specific operations for chain clients
type GatewayOperations interface {
	// GetLatestBlock returns the latest block/slot number
	GetLatestBlock(ctx context.Context) (uint64, error)

	// WatchGatewayEvents starts watching for gateway events from a specific block
	WatchGatewayEvents(ctx context.Context, fromBlock uint64) (<-chan *GatewayEvent, error)

	// GetTransactionConfirmations returns the number of confirmations for a transaction
	GetTransactionConfirmations(ctx context.Context, txHash string) (uint64, error)

	// IsConfirmed checks if a transaction has enough confirmations
	IsConfirmed(ctx context.Context, txHash string) (bool, error)
}
