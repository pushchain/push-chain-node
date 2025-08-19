package common

import (
	"context"

	uregistrytypes "github.com/rollchains/pchain/x/uregistry/types"
)

// ChainClient defines the interface for chain-specific implementations
type ChainClient interface {
	// ChainID returns the CAIP-2 format chain identifier
	ChainID() string

	// Start initializes and starts the chain client
	Start(ctx context.Context) error

	// Stop gracefully shuts down the chain client
	Stop() error

	// IsHealthy checks if the chain client is operational
	IsHealthy() bool

	// GetConfig returns the chain configuration
	GetConfig() *uregistrytypes.ChainConfig

	// Gateway operations (optional - clients can implement GatewayOperations)
	GatewayOperations
}

// BaseChainClient provides common functionality for all chain implementations
type BaseChainClient struct {
	config *uregistrytypes.ChainConfig
	ctx    context.Context
	cancel context.CancelFunc
}

// NewBaseChainClient creates a new base chain client
func NewBaseChainClient(config *uregistrytypes.ChainConfig) *BaseChainClient {
	return &BaseChainClient{
		config: config,
	}
}

// ChainID returns the CAIP-2 format chain identifier
func (b *BaseChainClient) ChainID() string {
	if b.config != nil {
		return b.config.Chain
	}
	return ""
}

// GetConfig returns the chain configuration
func (b *BaseChainClient) GetConfig() *uregistrytypes.ChainConfig {
	return b.config
}

// SetContext sets the context for the chain client
func (b *BaseChainClient) SetContext(ctx context.Context) {
	b.ctx, b.cancel = context.WithCancel(ctx)
}

// Context returns the chain client's context
func (b *BaseChainClient) Context() context.Context {
	return b.ctx
}

// Cancel cancels the chain client's context
func (b *BaseChainClient) Cancel() {
	if b.cancel != nil {
		b.cancel()
	}
}