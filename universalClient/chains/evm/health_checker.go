package evm

import (
	"context"
	"fmt"

	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/rollchains/pchain/universalClient/chains/common"
)

// EVMHealthChecker implements health checking for EVM endpoints
type EVMHealthChecker struct {
	expectedChainID int64
}

// NewEVMHealthChecker creates a new EVM health checker
func NewEVMHealthChecker(expectedChainID int64) *EVMHealthChecker {
	return &EVMHealthChecker{
		expectedChainID: expectedChainID,
	}
}

// CheckHealth performs a health check on an EVM client
func (h *EVMHealthChecker) CheckHealth(ctx context.Context, client interface{}) error {
	ethClient, ok := client.(*ethclient.Client)
	if !ok {
		return fmt.Errorf("invalid client type for EVM health check: %T", client)
	}

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

// Verify that EVMHealthChecker implements HealthChecker interface
var _ common.HealthChecker = (*EVMHealthChecker)(nil)