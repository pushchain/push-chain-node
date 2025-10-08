package svm

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gagliardetto/solana-go/rpc"
	"github.com/rs/zerolog"

	"github.com/pushchain/push-chain-node/universalClient/chains/common"
	"github.com/pushchain/push-chain-node/universalClient/config"
	"github.com/pushchain/push-chain-node/universalClient/db"
	"github.com/pushchain/push-chain-node/universalClient/rpcpool"
	uregistrytypes "github.com/pushchain/push-chain-node/x/uregistry/types"
)

// Client implements the ChainClient interface for Solana chains
type Client struct {
	*common.BaseChainClient
	logger         zerolog.Logger
	genesisHash    string           // Genesis hash extracted from CAIP-2
	rpcPool        *rpcpool.Manager // Pool manager for RPC endpoints
	gatewayHandler *GatewayHandler
	database       *db.DB
	appConfig      *config.Config
	retryManager   *common.RetryManager
	voteHandler    common.VoteHandler // Optional vote handler
	stopCh         chan struct{}
}

// NewClient creates a new Solana chain client
func NewClient(config *uregistrytypes.ChainConfig, database *db.DB, appConfig *config.Config, logger zerolog.Logger) (*Client, error) {
	if config == nil {
		return nil, fmt.Errorf("config is nil")
	}

	if config.VmType != uregistrytypes.VmType_SVM {
		return nil, fmt.Errorf("invalid VM type for Solana client: %v", config.VmType)
	}

	// Parse CAIP-2 chain ID (e.g., "solana:EtWTRABZaYq6iMfeYKouRu166VU2xqa1")
	genesisHash, err := parseSolanaChainID(config.Chain)
	if err != nil {
		return nil, fmt.Errorf("failed to parse chain ID: %w", err)
	}

	client := &Client{
		BaseChainClient: common.NewBaseChainClient(config, appConfig),
		logger: logger.With().
			Str("component", "svm_client").
			Str("chain", config.Chain).
			Logger(),
		genesisHash:  genesisHash,
		database:     database,
		appConfig:    appConfig,
		retryManager: common.NewRetryManager(nil, logger),
		stopCh:       make(chan struct{}),
	}

	return client, nil
}

// getRPCURLs returns the list of RPC URLs to use for this chain
func (c *Client) getRPCURLs() []string {
	// Use the base client's GetRPCURLs method
	urls := c.BaseChainClient.GetRPCURLs()

	if len(urls) > 0 {
		c.logger.Info().
			Str("chain", c.GetConfig().Chain).
			Int("url_count", len(urls)).
			Msg("using RPC URLs from local configuration")
		return urls
	}

	chainName := ""
	if c.GetConfig() != nil {
		chainName = c.GetConfig().Chain
	}
	c.logger.Warn().
		Str("chain", chainName).
		Msg("no RPC URLs configured for chain in local config")
	return []string{}
}

// getRPCClient returns an RPC client from the pool
func (c *Client) getRPCClient() (*rpc.Client, error) {
	if c.rpcPool == nil {
		return nil, fmt.Errorf("RPC pool not initialized")
	}

	endpoint, err := c.rpcPool.SelectEndpoint()
	if err != nil {
		return nil, fmt.Errorf("failed to select endpoint from pool: %w", err)
	}

	// Use the helper function from pool_adapter.go
	return GetSolanaClientFromPool(endpoint)
}

// executeWithFailover executes a function with automatic failover across RPC endpoints
func (c *Client) executeWithFailover(ctx context.Context, operation string, fn func(*rpc.Client) error) error {
	if c.rpcPool == nil {
		return fmt.Errorf("RPC pool not initialized for %s", operation)
	}

	// Use pool with automatic failover
	maxAttempts := 3 // Limit attempts to avoid infinite loops

	for attempt := 0; attempt < maxAttempts; attempt++ {
		endpoint, err := c.rpcPool.SelectEndpoint()
		if err != nil {
			return fmt.Errorf("no healthy endpoints available for %s: %w", operation, err)
		}

		// Use the helper function from pool_adapter.go
		rpcClient, err := GetSolanaClientFromPool(endpoint)
		if err != nil {
			c.logger.Error().
				Str("operation", operation).
				Str("url", endpoint.URL).
				Err(err).
				Msg("failed to get Solana client from endpoint")
			continue
		}

		start := time.Now()
		err = fn(rpcClient)
		latency := time.Since(start)

		if err == nil {
			// Success - update metrics and return
			c.rpcPool.UpdateEndpointMetrics(endpoint, true, latency, nil)
			c.logger.Debug().
				Str("operation", operation).
				Str("url", endpoint.URL).
				Dur("latency", latency).
				Int("attempt", attempt+1).
				Msg("operation completed successfully")
			return nil
		}

		// Failure - update metrics and try next endpoint
		c.rpcPool.UpdateEndpointMetrics(endpoint, false, latency, err)
		c.logger.Warn().
			Str("operation", operation).
			Str("url", endpoint.URL).
			Dur("latency", latency).
			Int("attempt", attempt+1).
			Err(err).
			Msg("operation failed, trying next endpoint")
	}

	return fmt.Errorf("operation %s failed after %d attempts", operation, maxAttempts)
}

// Start initializes and starts the Solana chain client
func (c *Client) Start(ctx context.Context) error {
	// Create a long-lived context for this client
	// Don't use the passed context directly as it may be short-lived
	clientCtx := context.Background()
	c.SetContext(clientCtx)

	// Get RPC URLs for this chain
	rpcURLs := c.getRPCURLs()
	if len(rpcURLs) == 0 {
		return fmt.Errorf("no RPC URLs configured for chain %s", c.GetConfig().Chain)
	}

	c.logger.Info().
		Str("genesis_hash", c.genesisHash).
		Int("rpc_url_count", len(rpcURLs)).
		Strs("rpc_urls", rpcURLs).
		Msg("starting Solana chain client")

	// Connect with retry logic - use clientCtx for long-lived operations
	err := c.retryManager.ExecuteWithRetry(ctx, "initial_connection", func() error {
		return c.connect(clientCtx)
	})
	if err != nil {
		return fmt.Errorf("failed to establish initial connection: %w", err)
	}

	// RPCPool handles all connection monitoring now

	c.logger.Info().Msg("Solana chain client started successfully")

	// Initialize gateway handler if gateway is configured
	if c.GetConfig() != nil && c.GetConfig().GatewayAddress != "" {
		// Create gateway handler with parent client reference for pool access
		handler, err := NewGatewayHandler(
			c, // Pass the client instance for RPC pool access
			c.GetConfig(),
			c.database,
			c.appConfig,
			c.logger,
		)
		if err != nil {
			c.logger.Warn().Err(err).Msg("failed to create gateway handler")
			// Not a fatal error - continue without gateway support
		} else {
			c.gatewayHandler = handler

			// Set vote handler if available
			if c.voteHandler != nil {
				c.gatewayHandler.SetVoteHandler(c.voteHandler)
				c.logger.Info().Msg("vote handler set on gateway handler during initialization")
			}
			c.logger.Info().
				Str("gateway_address", c.GetConfig().GatewayAddress).
				Msg("gateway handler initialized")

			// Start watching for gateway events in background
			go c.watchGatewayEvents()
		}
	}

	return nil
}

// watchGatewayEvents starts watching for gateway events in the background
func (c *Client) watchGatewayEvents() {
	c.logger.Info().
		Str("gateway_address", c.GetConfig().GatewayAddress).
		Msg("starting gateway event watcher")

	// Use the client's own context which is long-lived
	ctx := c.Context()
	if ctx == nil {
		c.logger.Error().Msg("client context is nil, cannot start event watcher")
		return
	}

	for {
		select {
		case <-ctx.Done():
			c.logger.Info().Msg("stopping gateway event watcher: context done")
			return
		case <-c.stopCh:
			c.logger.Info().Msg("stopping gateway event watcher: stop signal")
			return
		default:
			// Check if we have available endpoints
			if c.rpcPool == nil {
				c.logger.Debug().Msg("waiting for RPC pool to be initialized")
				time.Sleep(5 * time.Second)
				continue
			}

			if c.rpcPool.GetHealthyEndpointCount() == 0 {
				c.logger.Debug().Msg("waiting for healthy endpoints")
				time.Sleep(5 * time.Second)
				continue
			}

			// Check if gateway handler is available
			if c.gatewayHandler == nil {
				c.logger.Error().Msg("gateway handler is not initialized")
				return
			}

			// Get the starting slot from database
			startSlot, err := c.gatewayHandler.GetStartSlot(ctx)
			if err != nil {
				c.logger.Error().Err(err).Msg("failed to get start slot")
				time.Sleep(5 * time.Second)
				continue
			}

			// Log the starting point
			c.logger.Info().
				Uint64("start_slot", startSlot).
				Msg("determined starting slot from database")

			// Determine starting slot: per-chain config override, else DB start
			fromSlot := startSlot
			if c.appConfig != nil {
				if chainCfg := c.GetChainSpecificConfig(); chainCfg != nil && chainCfg.EventStartFrom != nil {
					if *chainCfg.EventStartFrom >= 0 {
						fromSlot = uint64(*chainCfg.EventStartFrom)
						c.logger.Info().Uint64("from_slot", fromSlot).Msg("using per-chain configured start slot")
					} else {
						// -1 means start from latest slot
						latest, latestErr := c.gatewayHandler.GetLatestBlock(ctx)
						if latestErr == nil {
							fromSlot = latest
							c.logger.Info().Uint64("from_slot", fromSlot).Msg("using latest slot as start (per-chain config -1)")
						} else {
							c.logger.Warn().Err(latestErr).Uint64("fallback_from_slot", fromSlot).Msg("failed to get latest slot; falling back to DB start slot")
						}
					}
				}
			}

			// Start watching events
			eventChan, err := c.WatchGatewayEvents(ctx, fromSlot)
			if err != nil {
				c.logger.Error().Err(err).Msg("failed to start watching gateway events")
				time.Sleep(5 * time.Second)
				continue
			}

			c.logger.Info().
				Uint64("from_slot", fromSlot).
				Msg("gateway event watcher started")

			// Process events until error or disconnection
			watchErr := c.processGatewayEvents(ctx, eventChan)
			if watchErr != nil {
				c.logger.Error().Err(watchErr).Msg("gateway event processing error")
				time.Sleep(5 * time.Second)
			}
		}
	}
}

// processGatewayEvents processes events from the event channel
func (c *Client) processGatewayEvents(ctx context.Context, eventChan <-chan *common.GatewayEvent) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-c.stopCh:
			return fmt.Errorf("stop signal received")
		case event, ok := <-eventChan:
			if !ok {
				return fmt.Errorf("event channel closed")
			}
			if event != nil {
				c.logger.Info().
					Str("tx_hash", event.TxHash).
					Uint64("slot", event.BlockNumber).
					Msg("received gateway event")

				// TODO: Process the event - e.g., send to a queue, update state, etc.
				// For now, we're just logging it
			}
		}
	}
}

// connect establishes connection to the Solana RPC endpoint(s)
func (c *Client) connect(ctx context.Context) error {
	rpcURLs := c.getRPCURLs()

	if len(rpcURLs) == 0 {
		return fmt.Errorf("no RPC URLs configured")
	}

	// Always use pool manager for consistency
	// Note: ctx here should be the long-lived clientCtx passed from Start()
	return c.initializeRPCPool(ctx, rpcURLs)
}

// initializeRPCPool creates and initializes the RPC pool manager
func (c *Client) initializeRPCPool(ctx context.Context, rpcURLs []string) error {
	c.logger.Info().
		Int("url_count", len(rpcURLs)).
		Msg("initializing Solana RPC pool")

	// Create pool manager using the new rpcpool module
	var poolConfig *config.RPCPoolConfig
	if c.appConfig != nil {
		poolConfig = &c.appConfig.RPCPoolConfig
	}
	c.rpcPool = rpcpool.NewManager(
		c.GetConfig().Chain,
		rpcURLs,
		poolConfig,
		CreateSVMClientFactory(),
		c.logger,
	)

	if c.rpcPool == nil {
		return fmt.Errorf("failed to create RPC pool manager")
	}

	// Set up health checker
	healthChecker := CreateSVMHealthChecker(c.genesisHash)
	c.rpcPool.HealthMonitor.SetHealthChecker(healthChecker)

	// Start the pool with the long-lived context
	// Note: ctx here should be the long-lived clientCtx passed from Start() -> connect()
	if err := c.rpcPool.Start(ctx); err != nil {
		return fmt.Errorf("failed to start RPC pool: %w", err)
	}

	c.logger.Info().
		Int("healthy_endpoints", c.rpcPool.GetHealthyEndpointCount()).
		Msg("Solana RPC pool initialized successfully")

	return nil
}

// SetVoteHandler sets the vote handler for confirmed transactions
func (c *Client) SetVoteHandler(handler common.VoteHandler) {
	c.voteHandler = handler
	if c.gatewayHandler != nil {
		c.gatewayHandler.SetVoteHandler(handler)
		c.logger.Info().Msg("vote handler set on Solana client and gateway handler")
	} else {
		c.logger.Debug().Msg("vote handler stored, will be set on gateway handler during start")
	}
}

// Stop gracefully shuts down the Solana chain client
func (c *Client) Stop() error {
	c.logger.Info().Msg("stopping Solana chain client")

	// Signal stop to all components
	close(c.stopCh)

	// Stop RPC pool
	if c.rpcPool != nil {
		c.rpcPool.Stop()
		c.rpcPool = nil
	}

	// Cancel context
	c.Cancel()

	c.logger.Info().Msg("Solana chain client stopped")
	return nil
}

// IsHealthy checks if the Solana chain client is operational
func (c *Client) IsHealthy() bool {
	if c.Context() == nil {
		return false
	}

	select {
	case <-c.Context().Done():
		return false
	default:
		// Check if we have healthy endpoints in the pool
		if c.rpcPool == nil {
			return false
		}

		healthyCount := c.rpcPool.GetHealthyEndpointCount()
		minHealthy := 1 // Default minimum
		if c.appConfig != nil {
			minHealthy = c.appConfig.RPCPoolConfig.MinHealthyEndpoints
		}
		return healthyCount >= minHealthy
	}
}

// GetGenesisHash returns the genesis hash
func (c *Client) GetGenesisHash() string {
	return c.genesisHash
}

// GetSlot returns the current slot
func (c *Client) GetSlot(ctx context.Context) (uint64, error) {
	var slot uint64

	err := c.executeWithFailover(ctx, "get_slot", func(client *rpc.Client) error {
		var err error
		slot, err = client.GetSlot(ctx, rpc.CommitmentFinalized)
		return err
	})

	if err != nil {
		return 0, fmt.Errorf("failed to get slot: %w", err)
	}

	return slot, nil
}

// GetRPCURL returns the first RPC endpoint URL from config or empty string
func (c *Client) GetRPCURL() string {
	urls := c.getRPCURLs()
	if len(urls) > 0 {
		return urls[0]
	}
	return ""
}

// parseSolanaChainID extracts the genesis hash from CAIP-2 format
func parseSolanaChainID(caip2 string) (string, error) {
	// Expected format: "solana:EtWTRABZaYq6iMfeYKouRu166VU2xqa1"
	parts := strings.Split(caip2, ":")
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid CAIP-2 format: %s", caip2)
	}

	if parts[0] != "solana" {
		return "", fmt.Errorf("not a Solana chain: %s", parts[0])
	}

	genesisHash := parts[1]
	if genesisHash == "" {
		return "", fmt.Errorf("empty genesis hash")
	}

	return genesisHash, nil
}

// Gateway operation implementations

// GetLatestBlock returns the latest slot number
func (c *Client) GetLatestBlock(ctx context.Context) (uint64, error) {
	if c.gatewayHandler != nil {
		return c.gatewayHandler.GetLatestBlock(ctx)
	}

	// Fallback to direct client call
	return c.GetSlot(ctx)
}

// WatchGatewayEvents starts watching for gateway events
func (c *Client) WatchGatewayEvents(ctx context.Context, fromBlock uint64) (<-chan *common.GatewayEvent, error) {
	if c.gatewayHandler == nil {
		return nil, fmt.Errorf("gateway handler not initialized")
	}
	return c.gatewayHandler.WatchGatewayEvents(ctx, fromBlock)
}

// GetTransactionConfirmations returns the number of confirmations for a transaction
func (c *Client) GetTransactionConfirmations(ctx context.Context, txHash string) (uint64, error) {
	if c.gatewayHandler == nil {
		return 0, fmt.Errorf("gateway handler not initialized")
	}
	return c.gatewayHandler.GetTransactionConfirmations(ctx, txHash)
}

// IsConfirmed checks if a transaction has enough confirmations
func (c *Client) IsConfirmed(ctx context.Context, txHash string) (bool, error) {
	if c.gatewayHandler == nil {
		return false, fmt.Errorf("gateway handler not initialized")
	}
	return c.gatewayHandler.IsConfirmed(ctx, txHash)
}
