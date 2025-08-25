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
	uregistrytypes "github.com/rollchains/pchain/x/uregistry/types"
)

// Client implements the ChainClient interface for Solana chains
type Client struct {
	*common.BaseChainClient
	logger         zerolog.Logger
	genesisHash    string // Genesis hash extracted from CAIP-2
	rpcURL         string
	rpcClient      *rpc.Client
	gatewayHandler *GatewayHandler
	database       *db.DB
	appConfig      *config.Config
	connMonitor    *common.ConnectionMonitor
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

	// Create connection monitor with reconnect handler
	client.connMonitor = common.NewConnectionMonitor(
		30*time.Second,
		client.reconnect,
		logger,
	)

	return client, nil
}

// Start initializes and starts the Solana chain client
func (c *Client) Start(ctx context.Context) error {
	// Create a long-lived context for this client
	// Don't use the passed context directly as it may be short-lived
	clientCtx := context.Background()
	c.SetContext(clientCtx)

	c.logger.Info().
		Str("genesis_hash", c.genesisHash).
		Str("rpc_url", c.rpcURL).
		Msg("starting Solana chain client")

	// Connect with retry logic
	err := c.retryManager.ExecuteWithRetry(ctx, "initial_connection", func() error {
		return c.connect(ctx)
	})
	if err != nil {
		return fmt.Errorf("failed to establish initial connection: %w", err)
	}

	// Start connection monitoring
	c.connMonitor.Start(clientCtx, c.healthCheck)

	c.logger.Info().Msg("Solana chain client started successfully")

	// Initialize gateway handler if gateway is configured
	if c.GetConfig() != nil && c.GetConfig().GatewayAddress != "" {
		handler, err := NewGatewayHandler(
			c.rpcClient,
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
				c.connMonitor.SetDisconnected()
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

// connect establishes connection to the Solana RPC endpoint
func (c *Client) connect(ctx context.Context) error {
	c.logger.Info().
		Str("rpc_url", c.rpcURL).
		Msg("connecting to Solana RPC endpoint")

	// Create Solana RPC client
	c.rpcClient = rpc.New(c.rpcURL)

	// Verify connection by getting health status
	health, err := c.rpcClient.GetHealth(ctx)
	if err != nil {
		return fmt.Errorf("failed to get health status: %w", err)
	}

	if health != "ok" {
		return fmt.Errorf("node is not healthy: %s", health)
	}

	c.connMonitor.SetConnected()
	
	c.logger.Info().
		Str("health", health).
		Msg("successfully connected to Solana RPC")

	return nil
}

// reconnect attempts to reconnect to the Solana RPC endpoint
func (c *Client) reconnect() error {
	c.logger.Info().Msg("attempting to reconnect to Solana RPC")

	// Solana RPC client doesn't need explicit close, just create new
	c.rpcClient = nil

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
			c.rpcClient,
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

	// Stop connection monitor
	if c.connMonitor != nil {
		c.connMonitor.Stop()
	}

	// Cancel context
	c.Cancel()

	// Solana RPC client doesn't need explicit close
	c.rpcClient = nil

	c.logger.Info().Msg("Solana chain client stopped")
	return nil
}

// IsHealthy checks if the Solana chain client is operational
func (c *Client) IsHealthy() bool {
	if c.Context() == nil || c.rpcClient == nil {
		return false
	}

	select {
	case <-c.Context().Done():
		return false
	default:
		// Check connection by getting health status
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