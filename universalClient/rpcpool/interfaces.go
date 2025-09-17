package rpcpool

import (
	"context"
)

// Client defines a generic interface for RPC clients that can be used in the pool
// Both EVM (*ethclient.Client) and SVM (*rpc.Client) clients implement this through adapters
type Client interface {
	// Ping performs a basic health check on the client
	Ping(ctx context.Context) error
	
	// Close closes the client connection
	Close() error
}

// ClientFactory creates chain-specific clients for a given URL
// This function is provided by each chain implementation (EVM, SVM) to create their specific client types
type ClientFactory func(url string) (Client, error)

// HealthChecker defines the interface for checking endpoint health
// Each chain type (EVM, SVM) implements this with chain-specific logic
type HealthChecker interface {
	CheckHealth(ctx context.Context, client Client) error
}