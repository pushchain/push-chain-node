package evm

import (
	"context"
	"fmt"

	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/rollchains/pchain/universalClient/rpcpool"
)

// evmClientAdapter wraps ethclient.Client to implement rpcpool.Client interface
type evmClientAdapter struct {
	client *ethclient.Client
}

// Ping performs a basic health check on the EVM client
func (a *evmClientAdapter) Ping(ctx context.Context) error {
	// Simple connectivity check - get the latest block number
	_, err := a.client.BlockNumber(ctx)
	return err
}

// Close closes the EVM client connection
func (a *evmClientAdapter) Close() error {
	a.client.Close()
	return nil
}

// GetEthClient returns the underlying ethclient.Client
func (a *evmClientAdapter) GetEthClient() *ethclient.Client {
	return a.client
}

// CreateEVMClientFactory returns a ClientFactory for EVM endpoints
func CreateEVMClientFactory() rpcpool.ClientFactory {
	return func(url string) (rpcpool.Client, error) {
		ethClient, err := ethclient.Dial(url)
		if err != nil {
			return nil, err
		}
		
		return &evmClientAdapter{
			client: ethClient,
		}, nil
	}
}

// CreateEVMHealthChecker creates a health checker for EVM endpoints
func CreateEVMHealthChecker(expectedChainID int64) rpcpool.HealthChecker {
	return &evmHealthChecker{
		expectedChainID: expectedChainID,
	}
}

// evmHealthChecker implements rpcpool.HealthChecker for EVM endpoints
type evmHealthChecker struct {
	expectedChainID int64
}

// CheckHealth performs comprehensive health checks on an EVM client
func (h *evmHealthChecker) CheckHealth(ctx context.Context, client rpcpool.Client) error {
	// Cast to our EVM adapter
	evmAdapter, ok := client.(*evmClientAdapter)
	if !ok {
		return fmt.Errorf("invalid client type for EVM health check: %T", client)
	}

	ethClient := evmAdapter.GetEthClient()

	// Check 1: Get current block number (tests basic connectivity)
	blockNumber, err := ethClient.BlockNumber(ctx)
	if err != nil {
		return fmt.Errorf("failed to get block number: %w", err)
	}

	// Basic sanity check - block number should be reasonable
	if blockNumber == 0 {
		return fmt.Errorf("block number is zero, chain may not be synced")
	}

	// Check 2: Verify chain ID (tests that we're connected to the right network)
	chainID, err := ethClient.ChainID(ctx)
	if err != nil {
		return fmt.Errorf("failed to get chain ID: %w", err)
	}

	if chainID.Int64() != h.expectedChainID {
		return fmt.Errorf("chain ID mismatch: expected %d, got %d", 
			h.expectedChainID, chainID.Int64())
	}

	return nil
}

// GetEthClientFromPool extracts the ethclient.Client from a pool endpoint
func GetEthClientFromPool(endpoint *rpcpool.Endpoint) (*ethclient.Client, error) {
	client := endpoint.GetClient()
	if client == nil {
		return nil, fmt.Errorf("no client available for endpoint %s", endpoint.URL)
	}

	evmAdapter, ok := client.(*evmClientAdapter)
	if !ok {
		return nil, fmt.Errorf("invalid client type: expected evmClientAdapter, got %T", client)
	}

	return evmAdapter.GetEthClient(), nil
}