package common

import (
	"context"
	"math/big"

	"github.com/pushchain/push-chain-node/universalClient/config"
	uregistrytypes "github.com/pushchain/push-chain-node/x/uregistry/types"
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

	// SetVoteHandler sets the vote handler for confirmed transactions
	SetVoteHandler(handler VoteHandler)

	// GetGasPrice fetches the current gas price from the chain
	GetGasPrice(ctx context.Context) (*big.Int, error)

	// Configuration access methods
	GetRPCURLs() []string
	GetCleanupSettings() (cleanupInterval, retentionPeriod int)
	GetGasPriceInterval() int
	GetChainSpecificConfig() *config.ChainSpecificConfig

	// Gateway operations (optional - clients can implement GatewayOperations)
	GatewayOperations
}

// BaseChainClient provides common functionality for all chain implementations
type BaseChainClient struct {
	config    *uregistrytypes.ChainConfig
	appConfig *config.Config
	ctx       context.Context
	cancel    context.CancelFunc
}

// NewBaseChainClient creates a new base chain client
func NewBaseChainClient(chainConfig *uregistrytypes.ChainConfig, appConfig *config.Config) *BaseChainClient {
	return &BaseChainClient{
		config:    chainConfig,
		appConfig: appConfig,
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

// GetRPCURLs returns the list of RPC URLs for this chain
func (b *BaseChainClient) GetRPCURLs() []string {
	if b.appConfig == nil || b.appConfig.ChainConfigs == nil {
		return []string{}
	}
	
	if b.config == nil {
		return []string{}
	}
	
	chainID := b.config.Chain
	if chainConfig, ok := b.appConfig.ChainConfigs[chainID]; ok {
		return chainConfig.RPCURLs
	}
	
	return []string{}
}

// GetCleanupSettings returns cleanup settings for this chain
// Falls back to global defaults if no chain-specific settings exist
func (b *BaseChainClient) GetCleanupSettings() (cleanupInterval, retentionPeriod int) {
	// Start with global defaults
	cleanupInterval = b.appConfig.TransactionCleanupIntervalSeconds
	retentionPeriod = b.appConfig.TransactionRetentionPeriodSeconds
	
	if b.appConfig == nil || b.appConfig.ChainConfigs == nil || b.config == nil {
		return cleanupInterval, retentionPeriod
	}
	
	chainID := b.config.Chain
	if chainConfig, ok := b.appConfig.ChainConfigs[chainID]; ok {
		// Override with chain-specific values if provided
		if chainConfig.CleanupIntervalSeconds != nil {
			cleanupInterval = *chainConfig.CleanupIntervalSeconds
		}
		if chainConfig.RetentionPeriodSeconds != nil {
			retentionPeriod = *chainConfig.RetentionPeriodSeconds
		}
	}
	
	return cleanupInterval, retentionPeriod
}

// GetGasPriceInterval returns the gas price fetch interval for this chain
// Defaults to 60 seconds if not configured
func (b *BaseChainClient) GetGasPriceInterval() int {
	// Default to 60 seconds
	defaultInterval := 60
	
	if b.appConfig == nil || b.appConfig.ChainConfigs == nil || b.config == nil {
		return defaultInterval
	}
	
	chainID := b.config.Chain
	if chainConfig, ok := b.appConfig.ChainConfigs[chainID]; ok {
		if chainConfig.GasPriceIntervalSeconds != nil {
			return *chainConfig.GasPriceIntervalSeconds
		}
	}
	
	return defaultInterval
}

// GetChainSpecificConfig returns the complete configuration for this chain
func (b *BaseChainClient) GetChainSpecificConfig() *config.ChainSpecificConfig {
	if b.appConfig == nil || b.appConfig.ChainConfigs == nil || b.config == nil {
		return &config.ChainSpecificConfig{}
	}
	
	chainID := b.config.Chain
	if chainConfig, ok := b.appConfig.ChainConfigs[chainID]; ok {
		return &chainConfig
	}
	
	// Return empty config if not found
	return &config.ChainSpecificConfig{}
}
