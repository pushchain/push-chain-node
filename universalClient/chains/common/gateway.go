package common

import (
	"context"
)

// GatewayEvent represents a cross-chain gateway event
type GatewayEvent struct {
	ChainID          string
	TxHash           string
	BlockNumber      uint64
	Method           string
	EventID          string
	Payload          []byte
	Confirmations    uint64
	ConfirmationType string // "STANDARD" or "FAST" - from gateway method config
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
