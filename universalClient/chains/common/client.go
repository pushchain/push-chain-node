package common

import (
	"context"
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
