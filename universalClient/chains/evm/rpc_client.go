package evm

import (
	"context"
	"fmt"
	"math/big"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum"
	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/rs/zerolog"
)

// RPCClient provides EVM-specific RPC operations
type RPCClient struct {
	clients []*ethclient.Client
	index   uint64
	mu      sync.RWMutex
	logger  zerolog.Logger
}

// NewRPCClient creates a new EVM RPC client from RPC URLs and validates chain ID
func NewRPCClient(rpcURLs []string, expectedChainID int64, logger zerolog.Logger) (*RPCClient, error) {
	if len(rpcURLs) == 0 {
		return nil, fmt.Errorf("no RPC URLs provided")
	}

	log := logger.With().Str("component", "evm_rpc_client").Logger()
	clients := make([]*ethclient.Client, 0, len(rpcURLs))

	// Create a temporary context for initial connection and chain ID verification
	// Use longer timeout for chain ID verification (30 seconds)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	for _, url := range rpcURLs {
		client, err := ethclient.DialContext(ctx, url)
		if err != nil {
			log.Warn().Err(err).Str("url", url).Msg("failed to connect to RPC endpoint, skipping")
			continue
		}

		// Verify chain ID matches (with timeout handling)
		clientChainID, err := client.ChainID(ctx)
		if err != nil {
			// If chain ID verification fails (timeout or error), log warning but still add client
			// This allows the system to continue even if verification is slow/unavailable
			log.Warn().
				Err(err).
				Str("url", url).
				Int64("expected_chain_id", expectedChainID).
				Msg("failed to verify chain ID (timeout or error), proceeding with client anyway")
			clients = append(clients, client)
			log.Info().Str("url", url).Msg("connected to RPC endpoint (chain ID verification skipped)")
			continue
		}

		if clientChainID.Int64() != expectedChainID {
			client.Close()
			log.Warn().
				Str("url", url).
				Int64("expected_chain_id", expectedChainID).
				Int64("actual_chain_id", clientChainID.Int64()).
				Msg("chain ID mismatch, closing client")
			continue
		}

		clients = append(clients, client)
		log.Info().Str("url", url).Msg("connected to RPC endpoint")
	}

	if len(clients) == 0 {
		return nil, fmt.Errorf("failed to connect to any valid RPC endpoints")
	}

	return &RPCClient{
		clients: clients,
		logger:  log,
	}, nil
}

// executeWithFailover executes a function with round-robin failover
func (rc *RPCClient) executeWithFailover(ctx context.Context, operation string, fn func(*ethclient.Client) error) error {
	rc.mu.RLock()
	clients := rc.clients
	rc.mu.RUnlock()

	if len(clients) == 0 {
		return fmt.Errorf("no RPC clients available for %s", operation)
	}

	maxAttempts := len(clients)
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if ctx != nil {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
		}

		index := atomic.AddUint64(&rc.index, 1) - 1
		client := clients[index%uint64(len(clients))]

		if client == nil {
			continue
		}

		err := fn(client)
		if err == nil {
			return nil
		}

		rc.logger.Warn().
			Str("operation", operation).
			Int("attempt", attempt+1).
			Err(err).
			Msg("operation failed, trying next endpoint")
	}

	return fmt.Errorf("operation %s failed after trying %d endpoints", operation, maxAttempts)
}

// IsHealthy checks if any RPC in the pool is healthy by pinging it
func (rc *RPCClient) IsHealthy(ctx context.Context) bool {
	rc.mu.RLock()
	hasClients := len(rc.clients) > 0
	rc.mu.RUnlock()

	if !hasClients {
		return false
	}

	_, err := rc.GetLatestBlock(ctx)
	return err == nil
}

// GetLatestBlock returns the latest block number
func (rc *RPCClient) GetLatestBlock(ctx context.Context) (uint64, error) {
	var blockNum uint64
	err := rc.executeWithFailover(ctx, "get_block_number", func(client *ethclient.Client) error {
		var innerErr error
		blockNum, innerErr = client.BlockNumber(ctx)
		return innerErr
	})
	return blockNum, err
}

// GetGasPrice fetches the current gas price
func (rc *RPCClient) GetGasPrice(ctx context.Context) (*big.Int, error) {
	var gasPrice *big.Int
	err := rc.executeWithFailover(ctx, "get_gas_price", func(client *ethclient.Client) error {
		callCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		var innerErr error
		gasPrice, innerErr = client.SuggestGasPrice(callCtx)
		return innerErr
	})
	return gasPrice, err
}

// FilterLogs fetches logs matching the filter query
func (rc *RPCClient) FilterLogs(ctx context.Context, query ethereum.FilterQuery) ([]types.Log, error) {
	var logs []types.Log
	err := rc.executeWithFailover(ctx, "filter_logs", func(client *ethclient.Client) error {
		var innerErr error
		logs, innerErr = client.FilterLogs(ctx, query)
		return innerErr
	})
	return logs, err
}

// GetTransactionReceipt fetches a transaction receipt
func (rc *RPCClient) GetTransactionReceipt(ctx context.Context, txHash ethcommon.Hash) (*types.Receipt, error) {
	var receipt *types.Receipt
	err := rc.executeWithFailover(ctx, "get_transaction_receipt", func(client *ethclient.Client) error {
		var innerErr error
		receipt, innerErr = client.TransactionReceipt(ctx, txHash)
		return innerErr
	})
	return receipt, err
}

// Close closes all RPC connections
func (rc *RPCClient) Close() {
	rc.mu.Lock()
	defer rc.mu.Unlock()

	for _, client := range rc.clients {
		if client != nil {
			client.Close()
		}
	}
	rc.clients = nil
}
