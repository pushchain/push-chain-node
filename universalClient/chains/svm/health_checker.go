package svm

import (
	"context"
	"fmt"

	"github.com/gagliardetto/solana-go/rpc"
)

// SVMHealthChecker implements health checking for Solana endpoints
type SVMHealthChecker struct {
	expectedGenesisHash string
}

// NewSVMHealthChecker creates a new SVM health checker
func NewSVMHealthChecker(expectedGenesisHash string) *SVMHealthChecker {
	return &SVMHealthChecker{
		expectedGenesisHash: expectedGenesisHash,
	}
}

// CheckHealth performs a health check on a Solana RPC client
func (h *SVMHealthChecker) CheckHealth(ctx context.Context, client interface{}) error {
	rpcClient, ok := client.(*rpc.Client)
	if !ok {
		return fmt.Errorf("invalid client type for SVM health check: %T", client)
	}

	// Check 1: Get health status
	health, err := rpcClient.GetHealth(ctx)
	if err != nil {
		return fmt.Errorf("failed to get health status: %w", err)
	}

	if health != "ok" {
		return fmt.Errorf("node is not healthy: %s", health)
	}

	// Check 2: Get slot (equivalent to block number, tests basic connectivity)
	slot, err := rpcClient.GetSlot(ctx, rpc.CommitmentConfirmed)
	if err != nil {
		return fmt.Errorf("failed to get slot: %w", err)
	}

	// Basic sanity check - slot should be reasonable
	if slot == 0 {
		return fmt.Errorf("slot is zero, chain may not be synced")
	}

	// Check 3: Verify genesis hash (tests that we're connected to the right network)
	if h.expectedGenesisHash != "" {
		genesisHash, err := rpcClient.GetGenesisHash(ctx)
		if err != nil {
			return fmt.Errorf("failed to get genesis hash: %w", err)
		}

		actualHash := genesisHash.String()
		// CAIP-2 standard uses truncated genesis hash (first 32 chars)
		// so we need to compare only the truncated portion
		if len(actualHash) > len(h.expectedGenesisHash) {
			actualHash = actualHash[:len(h.expectedGenesisHash)]
		}

		if actualHash != h.expectedGenesisHash {
			return fmt.Errorf("genesis hash mismatch: expected %s, got %s", 
				h.expectedGenesisHash, genesisHash.String())
		}
	}

	return nil
}