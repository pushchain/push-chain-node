package evm

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/rs/zerolog"

	"github.com/pushchain/push-chain-node/universalClient/chains/common"
	"github.com/pushchain/push-chain-node/universalClient/config"
	"github.com/pushchain/push-chain-node/universalClient/db"
	"github.com/pushchain/push-chain-node/universalClient/pushsigner"
	uregistrytypes "github.com/pushchain/push-chain-node/x/uregistry/types"
)

// Client implements the ChainClient interface for EVM chains
type Client struct {
	// Core configuration
	logger         zerolog.Logger
	chainIDStr     string
	registryConfig *uregistrytypes.ChainConfig
	chainConfig    *config.ChainSpecificConfig

	// Infrastructure
	rpcClient *RPCClient
	database  *db.DB
	ctx       context.Context
	cancel    context.CancelFunc

	// Components
	eventListener  *EventListener
	eventProcessor *common.EventProcessor
	eventConfirmer *EventConfirmer
	gasOracle      *GasOracle
	txBuilder      *TxBuilder

	// Dependencies
	pushSigner *pushsigner.Signer
}

// NewClient creates a new EVM chain client
func NewClient(
	config *uregistrytypes.ChainConfig,
	database *db.DB,
	chainConfig *config.ChainSpecificConfig,
	pushSigner *pushsigner.Signer,
	logger zerolog.Logger,
) (*Client, error) {
	if config == nil {
		return nil, fmt.Errorf("config is nil")
	}

	if config.VmType != uregistrytypes.VmType_EVM {
		return nil, fmt.Errorf("invalid VM type for EVM client: %v", config.VmType)
	}

	chainIDStr := config.Chain
	log := logger.With().Str("component", "evm_client").Str("chain", chainIDStr).Logger()

	// Validate RPC URLs are configured
	if chainConfig == nil || len(chainConfig.RPCURLs) == 0 {
		return nil, fmt.Errorf("no RPC URLs configured for chain %s", chainIDStr)
	}

	client := &Client{
		logger:         log,
		chainIDStr:     chainIDStr,
		registryConfig: config,
		chainConfig:    chainConfig,
		database:       database,
		pushSigner:     pushSigner,
	}

	// Initialize components that don't require RPC client
	if pushSigner != nil {
		client.eventProcessor = common.NewEventProcessor(
			pushSigner,
			database,
			chainIDStr,
			log,
		)
	}

	return client, nil
}

// Start initializes and starts the EVM chain client
func (c *Client) Start(ctx context.Context) error {
	c.ctx, c.cancel = context.WithCancel(context.Background())

	c.logger.Info().Str("chain", c.chainIDStr).Msg("starting EVM chain client")

	// Initialize RPC client first (required for other components)
	if err := c.createRPCClient(); err != nil {
		return fmt.Errorf("failed to create RPC client: %w", err)
	}

	// Initialize components that require RPC client
	if err := c.initializeComponents(); err != nil {
		return fmt.Errorf("failed to initialize components: %w", err)
	}

	// Start all components
	if err := c.startComponents(); err != nil {
		return fmt.Errorf("failed to start components: %w", err)
	}

	c.logger.Info().Msg("EVM chain client started successfully")
	return nil
}

// Stop gracefully shuts down the EVM chain client
func (c *Client) Stop() error {
	c.logger.Info().Msg("stopping EVM chain client")

	// Cancel context first to signal shutdown
	if c.cancel != nil {
		c.cancel()
	}

	// Stop components in reverse order of initialization
	if c.eventListener != nil {
		if err := c.eventListener.Stop(); err != nil {
			c.logger.Error().Err(err).Msg("error stopping event listener")
		}
	}

	if c.eventConfirmer != nil {
		c.eventConfirmer.Stop()
	}

	if c.eventProcessor != nil {
		if err := c.eventProcessor.Stop(); err != nil {
			c.logger.Error().Err(err).Msg("error stopping event processor")
		}
	}

	if c.gasOracle != nil {
		c.gasOracle.Stop()
	}

	// Close RPC client last
	if c.rpcClient != nil {
		c.rpcClient.Close()
	}

	c.logger.Info().Msg("EVM chain client stopped")
	return nil
}

// IsHealthy checks if the EVM chain RPC client is healthy
func (c *Client) IsHealthy() bool {
	if c.rpcClient == nil {
		return false
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	return c.rpcClient.IsHealthy(ctx)
}

// ChainID returns the chain ID string
func (c *Client) ChainID() string {
	return c.chainIDStr
}

// GetConfig returns the registry chain config
func (c *Client) GetConfig() *uregistrytypes.ChainConfig {
	return c.registryConfig
}

// GetTxBuilder returns the OutboundTxBuilder for this chain
func (c *Client) GetTxBuilder() (common.OutboundTxBuilder, error) {
	if c.txBuilder == nil {
		return nil, fmt.Errorf("txBuilder not available for chain %s (gateway not configured)", c.chainIDStr)
	}
	return c.txBuilder, nil
}

// GetDB returns the database instance for this chain
func (c *Client) GetDB() *db.DB {
	return c.database
}

// initializeComponents creates all components that require the RPC client
func (c *Client) initializeComponents() error {
	// Create event listener if gateway is configured
	if c.registryConfig != nil && c.registryConfig.GatewayAddress != "" {
		// Extract necessary config values
		eventPollingSeconds := 5 // default
		if c.chainConfig != nil && c.chainConfig.EventPollingIntervalSeconds != nil && *c.chainConfig.EventPollingIntervalSeconds > 0 {
			eventPollingSeconds = *c.chainConfig.EventPollingIntervalSeconds
		}

		var eventStartFrom *int64
		if c.chainConfig != nil && c.chainConfig.EventStartFrom != nil {
			eventStartFrom = c.chainConfig.EventStartFrom
		}

		eventListener, err := NewEventListener(
			c.rpcClient,
			c.registryConfig.GatewayAddress,
			c.registryConfig.Chain,
			c.registryConfig.GatewayMethods,
			c.database,
			eventPollingSeconds,
			eventStartFrom,
			c.logger,
		)
		if err != nil {
			return fmt.Errorf("failed to create event listener: %w", err)
		}
		c.eventListener = eventListener
	}

	// Apply defaults for all configuration values
	config := c.applyDefaults()

	// Create event confirmer
	c.eventConfirmer = NewEventConfirmer(
		c.rpcClient,
		c.database,
		c.chainIDStr,
		config.eventPollingInterval,
		config.fastConfirmations,
		config.standardConfirmations,
		c.logger,
	)

	// Create gas oracle if pushSigner is available
	if c.pushSigner != nil {
		c.gasOracle = NewGasOracle(
			c.rpcClient,
			c.pushSigner,
			c.chainIDStr,
			config.gasPriceInterval,
			c.logger,
		)
	}

	// Create txBuilder if gateway is configured
	if c.registryConfig != nil && c.registryConfig.GatewayAddress != "" {
		// Parse chain ID to integer
		chainIDInt, err := parseEVMChainID(c.chainIDStr)
		if err != nil {
			return fmt.Errorf("failed to parse chain ID for txBuilder: %w", err)
		}

		txBuilder, err := NewTxBuilder(
			c.rpcClient,
			c.chainIDStr,
			chainIDInt,
			c.registryConfig.GatewayAddress,
			c.logger,
		)
		if err != nil {
			return fmt.Errorf("failed to create txBuilder: %w", err)
		}
		c.txBuilder = txBuilder
	}

	return nil
}

// startComponents starts all initialized components
func (c *Client) startComponents() error {
	if c.eventListener != nil {
		if err := c.eventListener.Start(c.ctx); err != nil {
			return fmt.Errorf("failed to start event listener: %w", err)
		}
	}

	if c.eventConfirmer != nil {
		if err := c.eventConfirmer.Start(c.ctx); err != nil {
			return fmt.Errorf("failed to start event confirmer: %w", err)
		}
	}

	if c.eventProcessor != nil {
		if err := c.eventProcessor.Start(c.ctx); err != nil {
			return fmt.Errorf("failed to start event processor: %w", err)
		}
	}

	if c.gasOracle != nil {
		if err := c.gasOracle.Start(c.ctx); err != nil {
			return fmt.Errorf("failed to start gas oracle: %w", err)
		}
	}

	return nil
}

// createRPCClient creates and initializes the RPC client
func (c *Client) createRPCClient() error {
	// Parse chain ID for validation
	chainID, err := parseEVMChainID(c.chainIDStr)
	if err != nil {
		return fmt.Errorf("failed to parse chain ID: %w", err)
	}

	// Create RPC client from URLs with chain ID validation
	rpcClient, err := NewRPCClient(c.chainConfig.RPCURLs, chainID, c.logger)
	if err != nil {
		return fmt.Errorf("failed to create RPC client: %w", err)
	}

	c.rpcClient = rpcClient
	c.logger.Info().Int("connected_count", len(rpcClient.clients)).Msg("EVM RPC clients initialized successfully")
	return nil
}

// componentConfig holds configuration values for components with defaults applied
type componentConfig struct {
	eventPollingInterval  int
	gasPriceInterval      int
	fastConfirmations     uint64
	standardConfirmations uint64
}

// applyDefaults applies default values to all component configuration
func (c *Client) applyDefaults() componentConfig {
	config := componentConfig{
		eventPollingInterval:  5,  // default
		gasPriceInterval:      30, // default
		fastConfirmations:     2,
		standardConfirmations: 12,
	}

	// Apply event polling interval
	if c.chainConfig != nil && c.chainConfig.EventPollingIntervalSeconds != nil && *c.chainConfig.EventPollingIntervalSeconds > 0 {
		config.eventPollingInterval = *c.chainConfig.EventPollingIntervalSeconds
	}

	// Apply gas price interval
	if c.chainConfig != nil && c.chainConfig.GasPriceIntervalSeconds != nil && *c.chainConfig.GasPriceIntervalSeconds > 0 {
		config.gasPriceInterval = *c.chainConfig.GasPriceIntervalSeconds
	}

	// Apply confirmation requirements
	if c.registryConfig != nil && c.registryConfig.BlockConfirmation != nil {
		config.fastConfirmations = uint64(c.registryConfig.BlockConfirmation.FastInbound)
		config.standardConfirmations = uint64(c.registryConfig.BlockConfirmation.StandardInbound)
	}

	return config
}

// parseEVMChainID extracts the numeric chain ID from CAIP-2 format
func parseEVMChainID(caip2 string) (int64, error) {
	// Expected format: "eip155:11155111"
	parts := strings.Split(caip2, ":")
	if len(parts) != 2 {
		return 0, fmt.Errorf("invalid CAIP-2 format: %s", caip2)
	}

	if parts[0] != "eip155" {
		return 0, fmt.Errorf("not an EVM chain: %s", parts[0])
	}

	var chainID int64
	if _, err := fmt.Sscanf(parts[1], "%d", &chainID); err != nil {
		return 0, fmt.Errorf("failed to parse chain ID: %w", err)
	}

	return chainID, nil
}
