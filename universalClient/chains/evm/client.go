package evm

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/rs/zerolog"

	"github.com/rollchains/pchain/universalClient/chains/common"
	"github.com/rollchains/pchain/universalClient/config"
	"github.com/rollchains/pchain/universalClient/db"
	uregistrytypes "github.com/rollchains/pchain/x/uregistry/types"
)

// Client implements the ChainClient interface for EVM chains
type Client struct {
	*common.BaseChainClient
	logger         zerolog.Logger
	chainID        int64 // Numeric chain ID extracted from CAIP-2
	rpcPool        *common.RPCPoolManager // Pool manager for multiple RPC endpoints
	rpcURL         string                 // Fallback single RPC URL (legacy)
	ethClient      *ethclient.Client      // Fallback single client (legacy)
	gatewayHandler *GatewayHandler
	database       *db.DB
	appConfig      *config.Config
	connMonitor    *common.ConnectionMonitor
	retryManager   *common.RetryManager
	stopCh         chan struct{}
}

// NewClient creates a new EVM chain client
func NewClient(config *uregistrytypes.ChainConfig, database *db.DB, appConfig *config.Config, logger zerolog.Logger) (*Client, error) {
	if config == nil {
		return nil, fmt.Errorf("config is nil")
	}

	if config.VmType != uregistrytypes.VmType_EVM {
		return nil, fmt.Errorf("invalid VM type for EVM client: %v", config.VmType)
	}

	// Parse CAIP-2 chain ID (e.g., "eip155:11155111")
	chainID, err := parseEVMChainID(config.Chain)
	if err != nil {
		return nil, fmt.Errorf("failed to parse chain ID: %w", err)
	}

	client := &Client{
		BaseChainClient: common.NewBaseChainClient(config),
		logger: logger.With().
			Str("component", "evm_client").
			Str("chain", config.Chain).
			Logger(),
		chainID:      chainID,
		rpcURL:       config.PublicRpcUrl,
		database:     database,
		appConfig:    appConfig,
		retryManager: common.NewRetryManager(nil, logger),
		stopCh:       make(chan struct{}),
	}

	// Create connection monitor with reconnect handler
	client.connMonitor = common.NewConnectionMonitor(
		30*time.Second,
		client.reconnect,
		logger,
	)

	return client, nil
}

// getRPCURLs returns the list of RPC URLs to use for this chain
func (c *Client) getRPCURLs() []string {
	chainConfig := c.GetConfig()
	if chainConfig == nil {
		return []string{}
	}

	// Only use local config ChainRPCURLs - no fallback to registry
	if urls, ok := c.appConfig.ChainRPCURLs[chainConfig.Chain]; ok && len(urls) > 0 {
		c.logger.Info().
			Str("chain", chainConfig.Chain).
			Int("url_count", len(urls)).
			Msg("using RPC URLs from local configuration")
		return urls
	}

	c.logger.Warn().
		Str("chain", chainConfig.Chain).
		Msg("no RPC URLs configured for chain in local config")
	return []string{}
}

// createEthClient creates an ethclient.Client for the given URL
func (c *Client) createEthClient(url string) (interface{}, error) {
	client, err := ethclient.Dial(url)
	if err != nil {
		return nil, fmt.Errorf("failed to create eth client for %s: %w", url, err)
	}
	return client, nil
}

// getEthClient returns an eth client, either from pool or fallback
func (c *Client) getEthClient() (*ethclient.Client, error) {
	if c.rpcPool != nil {
		endpoint, err := c.rpcPool.SelectEndpoint()
		if err != nil {
			return nil, fmt.Errorf("failed to select endpoint from pool: %w", err)
		}
		
		client := endpoint.GetClient()
		ethClient, ok := client.(*ethclient.Client)
		if !ok {
			return nil, fmt.Errorf("invalid client type in pool: %T", client)
		}
		
		return ethClient, nil
	}

	// Fallback to single client
	if c.ethClient == nil {
		return nil, fmt.Errorf("no eth client available")
	}
	
	return c.ethClient, nil
}

// executeWithFailover executes a function with automatic failover across RPC endpoints
func (c *Client) executeWithFailover(ctx context.Context, operation string, fn func(*ethclient.Client) error) error {
	if c.rpcPool != nil {
		// Use pool with automatic failover
		maxAttempts := 3 // Limit attempts to avoid infinite loops
		
		for attempt := 0; attempt < maxAttempts; attempt++ {
			endpoint, err := c.rpcPool.SelectEndpoint()
			if err != nil {
				return fmt.Errorf("no healthy endpoints available for %s: %w", operation, err)
			}
			
			client := endpoint.GetClient()
			ethClient, ok := client.(*ethclient.Client)
			if !ok {
				c.logger.Error().
					Str("operation", operation).
					Str("url", endpoint.URL).
					Msg("invalid client type in endpoint")
				continue
			}
			
			start := time.Now()
			err = fn(ethClient)
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
	if c.ethClient == nil {
		return fmt.Errorf("no eth client available for %s", operation)
	}
	
	return fn(c.ethClient)
}

// Start initializes and starts the EVM chain client
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
		Int64("chain_id", c.chainID).
		Int("rpc_url_count", len(rpcURLs)).
		Strs("rpc_urls", rpcURLs).
		Msg("starting EVM chain client")

	// Connect with retry logic
	err := c.retryManager.ExecuteWithRetry(ctx, "initial_connection", func() error {
		return c.connect(ctx)
	})
	if err != nil {
		return fmt.Errorf("failed to establish initial connection: %w", err)
	}

	// Start connection monitoring (only if using single client fallback)
	if c.rpcPool == nil {
		c.connMonitor.Start(clientCtx, c.healthCheck)
	}

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
			// Check if connected
			if !c.connMonitor.IsConnected() {
				c.logger.Debug().Msg("waiting for connection to be established")
				time.Sleep(5 * time.Second)
				continue
			}

			// Get the starting block from database
			startBlock, err := c.gatewayHandler.GetStartBlock(ctx)
			if err != nil {
				c.logger.Error().Err(err).Msg("failed to get start block")
				time.Sleep(5 * time.Second)
				continue
			}
			
			// Log the starting point
			c.logger.Info().
				Uint64("start_block", startBlock).
				Msg("determined starting block from database")

			// Start watching events using long-lived context
			eventChan, err := c.WatchGatewayEvents(ctx, startBlock)
			if err != nil {
				c.logger.Error().Err(err).Msg("failed to start watching gateway events")
				c.connMonitor.SetDisconnected()
				time.Sleep(5 * time.Second)
				continue
			}

			c.logger.Info().
				Uint64("from_block", startBlock).
				Msg("gateway event watcher started")

			// Process events until error or disconnection
			watchErr := c.processGatewayEvents(ctx, eventChan)
			if watchErr != nil {
				c.logger.Error().Err(watchErr).Msg("gateway event processing error")
				c.connMonitor.SetDisconnected()
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
					Uint64("block", event.BlockNumber).
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

// connect establishes connection to the EVM RPC endpoint(s)
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
		Msg("initializing EVM RPC pool")

	// Create pool manager
	c.rpcPool = common.NewRPCPoolManager(
		c.GetConfig().Chain,
		rpcURLs,
		&c.appConfig.RPCPoolConfig,
		c.createEthClient,
		c.logger,
	)

	if c.rpcPool == nil {
		return fmt.Errorf("failed to create RPC pool manager")
	}

	// Set up health checker
	healthChecker := NewEVMHealthChecker(c.chainID)
	c.rpcPool.HealthMonitor.SetHealthChecker(healthChecker)

	// Start the pool
	if err := c.rpcPool.Start(ctx); err != nil {
		return fmt.Errorf("failed to start RPC pool: %w", err)
	}

	c.logger.Info().
		Int("healthy_endpoints", c.rpcPool.GetHealthyEndpointCount()).
		Msg("EVM RPC pool initialized successfully")

	return nil
}

// initializeSingleClient creates a traditional single client (legacy mode)
func (c *Client) initializeSingleClient(ctx context.Context, rpcURL string) error {
	c.logger.Info().
		Str("rpc_url", rpcURL).
		Msg("initializing single EVM RPC client (legacy mode)")

	c.rpcURL = rpcURL

	ethClient, err := ethclient.DialContext(ctx, rpcURL)
	if err != nil {
		return fmt.Errorf("failed to connect to EVM RPC: %w", err)
	}

	// Verify connection by getting chain ID
	chainID, err := ethClient.ChainID(ctx)
	if err != nil {
		ethClient.Close()
		return fmt.Errorf("failed to get chain ID: %w", err)
	}

	// Verify chain ID matches expected
	if chainID.Int64() != c.chainID {
		ethClient.Close()
		return fmt.Errorf("chain ID mismatch: expected %d, got %d", c.chainID, chainID.Int64())
	}

	c.ethClient = ethClient
	c.connMonitor.SetConnected()
	
	c.logger.Info().
		Int64("chain_id", chainID.Int64()).
		Msg("successfully connected to EVM RPC")

	return nil
}

// reconnect attempts to reconnect to the EVM RPC endpoint
func (c *Client) reconnect() error {
	c.logger.Info().Msg("attempting to reconnect to EVM RPC")

	// Close existing connection if any
	if c.ethClient != nil {
		c.ethClient.Close()
		c.ethClient = nil
	}

	// Use a fresh context for reconnection
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Attempt to connect
	if err := c.connect(ctx); err != nil {
		return fmt.Errorf("reconnection failed: %w", err)
	}

	// Reinitialize gateway handler if needed
	if c.gatewayHandler != nil && c.GetConfig() != nil && c.GetConfig().GatewayAddress != "" {
		c.logger.Info().Msg("reinitializing gateway handler after reconnection")
		
		handler, err := NewGatewayHandler(
			c, // Pass the client instance for RPC pool access
			c.GetConfig(),
			c.database,
			c.appConfig,
			c.logger,
		)
		if err != nil {
			c.logger.Warn().Err(err).Msg("failed to recreate gateway handler after reconnection")
		} else {
			c.gatewayHandler = handler
			c.logger.Info().Msg("gateway handler reinitialized successfully")
		}
	}

	return nil
}

// healthCheck performs a health check on the connection
func (c *Client) healthCheck() error {
	if c.ethClient == nil {
		return fmt.Errorf("client not connected")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := c.ethClient.BlockNumber(ctx)
	if err != nil {
		return fmt.Errorf("health check failed: %w", err)
	}

	return nil
}

// Stop gracefully shuts down the EVM chain client
func (c *Client) Stop() error {
	c.logger.Info().Msg("stopping EVM chain client")

	// Signal stop to all components
	close(c.stopCh)

	// Stop RPC pool if using it
	if c.rpcPool != nil {
		c.rpcPool.Stop()
		c.rpcPool = nil
	}

	// Stop connection monitor (for single client mode)
	if c.connMonitor != nil {
		c.connMonitor.Stop()
	}

	// Cancel context
	c.Cancel()

	// Close single ethclient connection (legacy mode)
	if c.ethClient != nil {
		c.ethClient.Close()
		c.ethClient = nil
	}

	c.logger.Info().Msg("EVM chain client stopped")
	return nil
}

// IsHealthy checks if the EVM chain client is operational
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
		if c.ethClient == nil {
			return false
		}
		
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		
		_, err := c.ethClient.BlockNumber(ctx)
		if err != nil {
			c.logger.Warn().Err(err).Msg("health check failed")
			return false
		}
		return true
	}
}

// GetChainID returns the numeric chain ID
func (c *Client) GetChainID() int64 {
	return c.chainID
}

// GetLatestBlockNumber returns the latest block number with automatic failover
func (c *Client) GetLatestBlockNumber(ctx context.Context) (*big.Int, error) {
	var blockNumber uint64
	var err error
	
	err = c.executeWithFailover(ctx, "get_block_number", func(client *ethclient.Client) error {
		blockNumber, err = client.BlockNumber(ctx)
		return err
	})
	
	if err != nil {
		return nil, fmt.Errorf("failed to get block number: %w", err)
	}
	
	return new(big.Int).SetUint64(blockNumber), nil
}

// GetRPCURL returns the RPC endpoint URL
func (c *Client) GetRPCURL() string {
	return c.rpcURL
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

// Gateway operation implementations

// GetLatestBlock returns the latest block number
func (c *Client) GetLatestBlock(ctx context.Context) (uint64, error) {
	if c.gatewayHandler != nil {
		return c.gatewayHandler.GetLatestBlock(ctx)
	}
	
	// Fallback to direct client call
	if c.ethClient == nil {
		return 0, fmt.Errorf("client not connected")
	}
	return c.ethClient.BlockNumber(ctx)
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