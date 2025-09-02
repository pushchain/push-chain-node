package svm

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gagliardetto/solana-go/rpc"
	"github.com/rs/zerolog"

	"github.com/pushchain/push-chain-node/universalClient/chains/common"
	uregistrytypes "github.com/pushchain/push-chain-node/x/uregistry/types"
)

// Client implements the ChainClient interface for Solana chains
type Client struct {
	*common.BaseChainClient
	logger      zerolog.Logger
	genesisHash string // Genesis hash extracted from CAIP-2
	rpcURL      string
	rpcClient   *rpc.Client
}

// NewClient creates a new Solana chain client
func NewClient(config *uregistrytypes.ChainConfig, logger zerolog.Logger) (*Client, error) {
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

	return &Client{
		BaseChainClient: common.NewBaseChainClient(config),
		logger: logger.With().
			Str("component", "solana_client").
			Str("chain", config.Chain).
			Logger(),
		genesisHash: genesisHash,
		rpcURL:      config.PublicRpcUrl,
	}, nil
}

// Start initializes and starts the Solana chain client
func (c *Client) Start(ctx context.Context) error {
	c.SetContext(ctx)

	c.logger.Info().
		Str("genesis_hash", c.genesisHash).
		Str("rpc_url", c.rpcURL).
		Msg("starting Solana chain client")

	// Create Solana RPC client
	c.rpcClient = rpc.New(c.rpcURL)

	// Verify connection by getting health status
	health, err := c.rpcClient.GetHealth(ctx)
	if err != nil {
		c.logger.Error().Err(err).Msg("failed to get health status")
		return fmt.Errorf("failed to get health status: %w", err)
	}

	if health != "ok" {
		c.logger.Error().Str("status", health).Msg("node is not healthy")
		return fmt.Errorf("node is not healthy: %s", health)
	}

	c.logger.Info().
		Str("health", health).
		Msg("Solana chain client started successfully")
	return nil
}

// Stop gracefully shuts down the Solana chain client
func (c *Client) Stop() error {
	c.logger.Info().Msg("stopping Solana chain client")

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
