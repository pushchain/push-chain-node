package evm

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/rs/zerolog"

	"github.com/pushchain/push-chain-node/universalClient/chains/common"
	uregistrytypes "github.com/pushchain/push-chain-node/x/uregistry/types"
)

// Client implements the ChainClient interface for EVM chains
type Client struct {
	*common.BaseChainClient
	logger    zerolog.Logger
	chainID   int64 // Numeric chain ID extracted from CAIP-2
	rpcURL    string
	ethClient *ethclient.Client
}

// NewClient creates a new EVM chain client
func NewClient(config *uregistrytypes.ChainConfig, logger zerolog.Logger) (*Client, error) {
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

	return &Client{
		BaseChainClient: common.NewBaseChainClient(config),
		logger: logger.With().
			Str("component", "evm_client").
			Str("chain", config.Chain).
			Logger(),
		chainID: chainID,
		rpcURL:  config.PublicRpcUrl,
	}, nil
}

// Start initializes and starts the EVM chain client
func (c *Client) Start(ctx context.Context) error {
	c.SetContext(ctx)

	c.logger.Info().
		Int64("chain_id", c.chainID).
		Str("rpc_url", c.rpcURL).
		Msg("starting EVM chain client")

	// Create ethclient connection
	client, err := ethclient.DialContext(ctx, c.rpcURL)
	if err != nil {
		c.logger.Error().Err(err).Msg("failed to connect to EVM RPC")
		return fmt.Errorf("failed to connect to RPC: %w", err)
	}
	c.ethClient = client

	// Verify connection by getting chain ID
	chainID, err := client.ChainID(ctx)
	if err != nil {
		c.logger.Error().Err(err).Msg("failed to get chain ID")
		return fmt.Errorf("failed to get chain ID: %w", err)
	}

	// Verify chain ID matches expected
	if chainID.Int64() != c.chainID {
		c.logger.Error().
			Int64("expected", c.chainID).
			Int64("actual", chainID.Int64()).
			Msg("chain ID mismatch")
		return fmt.Errorf("chain ID mismatch: expected %d, got %d", c.chainID, chainID.Int64())
	}

	c.logger.Info().
		Int64("chain_id", chainID.Int64()).
		Msg("EVM chain client started successfully")
	return nil
}

// Stop gracefully shuts down the EVM chain client
func (c *Client) Stop() error {
	c.logger.Info().Msg("stopping EVM chain client")

	// Cancel context
	c.Cancel()

	// Close ethclient connection
	if c.ethClient != nil {
		c.ethClient.Close()
		c.ethClient = nil
	}

	c.logger.Info().Msg("EVM chain client stopped")
	return nil
}

// IsHealthy checks if the EVM chain client is operational
func (c *Client) IsHealthy() bool {
	if c.Context() == nil || c.ethClient == nil {
		return false
	}

	select {
	case <-c.Context().Done():
		return false
	default:
		// Check connection by getting latest block number
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

// GetLatestBlockNumber returns the latest block number (placeholder for future use)
func (c *Client) GetLatestBlockNumber(ctx context.Context) (*big.Int, error) {
	if c.ethClient == nil {
		return nil, fmt.Errorf("client not connected")
	}
	blockNum, err := c.ethClient.BlockNumber(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get block number: %w", err)
	}
	return new(big.Int).SetUint64(blockNum), nil
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
