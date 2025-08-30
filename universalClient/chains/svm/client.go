package svm

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gagliardetto/solana-go/rpc"
	"github.com/rs/zerolog"

	"github.com/rollchains/pchain/universalClient/chains/common"
	"github.com/rollchains/pchain/universalClient/config"
	"github.com/rollchains/pchain/universalClient/db"
	"github.com/rollchains/pchain/universalClient/rpcpool"
	uregistrytypes "github.com/rollchains/pchain/x/uregistry/types"
)

// Client implements the ChainClient interface for Solana chains
type Client struct {
	*common.BaseChainClient
	logger         zerolog.Logger
	genesisHash    string           // Genesis hash extracted from CAIP-2
	rpcPool        *rpcpool.Manager // Pool manager for multiple RPC endpoints
	rpcURL         string                   // Fallback single RPC URL (legacy)
	rpcClient      *rpc.Client              // Fallback single client (legacy)
	gatewayHandler *GatewayHandler
	database       *db.DB
	appConfig      *config.Config
	retryManager   *common.RetryManager
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
		BaseChainClient: common.NewBaseChainClient(config),
		logger: logger.With().
			Str("component", "solana_client").
			Str("chain", config.Chain).
			Logger(),
		genesisHash:  genesisHash,
		rpcURL:       config.PublicRpcUrl,
		database:     database,
		appConfig:    appConfig,
		retryManager: common.NewRetryManager(nil, logger),
		stopCh:       make(chan struct{}),
	}


	return client, nil
}

// getRPCURLs returns the list of RPC URLs to use for this chain
func (c *Client) getRPCURLs() []string {
	// Only check common config if appConfig is not nil
	if c.appConfig != nil {
		urls := common.GetRPCURLs(c.GetConfig(), c.appConfig)
		
		if len(urls) > 0 {
			c.logger.Info().
				Str("chain", c.GetConfig().Chain).
				Int("url_count", len(urls)).
				Msg("using RPC URLs from local configuration")
			return urls
		}
	}

	// Fallback to PublicRpcUrl from chain config if available
	chainConfig := c.GetConfig()
	if chainConfig != nil && chainConfig.PublicRpcUrl != "" {
		c.logger.Info().
			Str("chain", chainConfig.Chain).
			Str("url", chainConfig.PublicRpcUrl).
			Msg("using PublicRpcUrl from chain config as fallback")
		return []string{chainConfig.PublicRpcUrl}
	}

	chainName := ""
	if c.GetConfig() != nil {
		chainName = c.GetConfig().Chain
	}
	c.logger.Warn().
		Str("chain", chainName).
		Msg("no RPC URLs configured for chain")
	return []string{}
}


// getRPCClient returns an RPC client, either from pool or fallback
func (c *Client) getRPCClient() (*rpc.Client, error) {
	if c.rpcPool != nil {
		endpoint, err := c.rpcPool.SelectEndpoint()
		if err != nil {
			return nil, fmt.Errorf("failed to select endpoint from pool: %w", err)
		}
		
		// Use the helper function from pool_adapter.go
		return GetSolanaClientFromPool(endpoint)
	}

	// Fallback to single client
	if c.rpcClient == nil {
		return nil, fmt.Errorf("no RPC client available")
	}
	
	return c.rpcClient, nil
}

// executeWithFailover executes a function with automatic failover across RPC endpoints
func (c *Client) executeWithFailover(ctx context.Context, operation string, fn func(*rpc.Client) error) error {
	if c.rpcPool != nil {
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

	// Fallback to single client
	if c.rpcClient == nil {
		return fmt.Errorf("no RPC client available for %s", operation)
	}
	
	return fn(c.rpcClient)
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

	// Connect with retry logic
	err := c.retryManager.ExecuteWithRetry(ctx, "initial_connection", func() error {
		return c.connect(ctx)
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
			if c.rpcPool != nil && c.rpcPool.GetHealthyEndpointCount() == 0 {
				c.logger.Debug().Msg("waiting for healthy endpoints")
				time.Sleep(5 * time.Second)
				continue
			} else if c.rpcPool == nil && c.rpcClient == nil {
				c.logger.Debug().Msg("waiting for connection to be established")
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

			// Start watching events
			eventChan, err := c.WatchGatewayEvents(ctx, startSlot)
			if err != nil {
				c.logger.Error().Err(err).Msg("failed to start watching gateway events")
				time.Sleep(5 * time.Second)
				continue
			}

			c.logger.Info().
				Uint64("from_slot", startSlot).
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
					Str("method", event.Method).
					Uint64("slot", event.BlockNumber).
					Str("sender", event.Sender).
					Str("receiver", event.Receiver).
					Str("amount", event.Amount).
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
	
	if len(rpcURLs) > 1 {
		// Multiple URLs - use pool manager
		return c.initializeRPCPool(ctx, rpcURLs)
	} else if len(rpcURLs) == 1 {
		// Single URL - use traditional client
		return c.initializeSingleClient(ctx, rpcURLs[0])
	}
	
	return fmt.Errorf("no RPC URLs configured")
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

	// Start the pool
	if err := c.rpcPool.Start(ctx); err != nil {
		return fmt.Errorf("failed to start RPC pool: %w", err)
	}

	c.logger.Info().
		Int("healthy_endpoints", c.rpcPool.GetHealthyEndpointCount()).
		Msg("Solana RPC pool initialized successfully")

	return nil
}

// initializeSingleClient creates a traditional single client (legacy mode)
func (c *Client) initializeSingleClient(ctx context.Context, rpcURL string) error {
	c.logger.Info().
		Str("rpc_url", rpcURL).
		Msg("initializing single Solana RPC client (legacy mode)")

	c.rpcURL = rpcURL

	// Create Solana RPC client
	c.rpcClient = rpc.New(rpcURL)

	// Verify connection by getting health status
	health, err := c.rpcClient.GetHealth(ctx)
	if err != nil {
		return fmt.Errorf("failed to get health status: %w", err)
	}

	if health != "ok" {
		return fmt.Errorf("node is not healthy: %s", health)
	}

	
	c.logger.Info().
		Str("health", health).
		Msg("successfully connected to Solana RPC")

	return nil
}


// healthCheck performs a health check on the connection
func (c *Client) healthCheck() error {
	if c.rpcClient == nil {
		return fmt.Errorf("client not connected")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	health, err := c.rpcClient.GetHealth(ctx)
	if err != nil {
		return fmt.Errorf("health check failed: %w", err)
	}

	if health != "ok" {
		return fmt.Errorf("node unhealthy: %s", health)
	}

	return nil
}

// Stop gracefully shuts down the Solana chain client
func (c *Client) Stop() error {
	c.logger.Info().Msg("stopping Solana chain client")

	// Signal stop to all components
	close(c.stopCh)

	// Stop RPC pool if using it
	if c.rpcPool != nil {
		c.rpcPool.Stop()
		c.rpcPool = nil
	}


	// Cancel context
	c.Cancel()

	// Solana RPC client doesn't need explicit close (legacy mode)
	c.rpcClient = nil

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
		// Check if we have healthy endpoints or a working single client
		if c.rpcPool != nil {
			healthyCount := c.rpcPool.GetHealthyEndpointCount()
			return healthyCount >= c.appConfig.RPCPoolConfig.MinHealthyEndpoints
		}
		
		// Fallback to single client health check
		if c.rpcClient == nil {
			return false
		}
		
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		
		health, err := c.rpcClient.GetHealth(ctx)
		if err != nil {
			c.logger.Warn().Err(err).Msg("health check failed")
			return false
		}
		return health == "ok"
	}
}

// GetGenesisHash returns the genesis hash
func (c *Client) GetGenesisHash() string {
	return c.genesisHash
}

// GetSlot returns the current slot (placeholder for future use)
func (c *Client) GetSlot(ctx context.Context) (uint64, error) {
	if c.rpcClient == nil {
		return 0, fmt.Errorf("client not connected")
	}
	
	slot, err := c.rpcClient.GetSlot(ctx, rpc.CommitmentFinalized)
	if err != nil {
		return 0, fmt.Errorf("failed to get slot: %w", err)
	}
	
	return slot, nil
}

// GetRPCURL returns the RPC endpoint URL
func (c *Client) GetRPCURL() string {
	return c.rpcURL
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
func (c *Client) IsConfirmed(ctx context.Context, txHash string, mode string) (bool, error) {
	if c.gatewayHandler == nil {
		return false, fmt.Errorf("gateway handler not initialized")
	}
	return c.gatewayHandler.IsConfirmed(ctx, txHash, mode)
}