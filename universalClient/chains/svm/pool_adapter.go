package svm

import (
	"context"
	"fmt"

	"github.com/gagliardetto/solana-go/rpc"
	"github.com/pushchain/push-chain-node/universalClient/rpcpool"
)

// svmClientAdapter wraps rpc.Client to implement rpcpool.Client interface
type svmClientAdapter struct {
	client *rpc.Client
}

// Ping performs a basic health check on the SVM client
func (a *svmClientAdapter) Ping(ctx context.Context) error {
	// Simple connectivity check - get the latest slot
	_, err := a.client.GetSlot(ctx, rpc.CommitmentConfirmed)
	return err
}

// Close closes the SVM client connection
func (a *svmClientAdapter) Close() error {
	// Solana RPC client doesn't have an explicit Close method
	// Setting to nil for garbage collection
	a.client = nil
	return nil
}

// GetSolanaClient returns the underlying rpc.Client
func (a *svmClientAdapter) GetSolanaClient() *rpc.Client {
	return a.client
}

// CreateSVMClientFactory returns a ClientFactory for SVM endpoints
func CreateSVMClientFactory() rpcpool.ClientFactory {
	return func(url string) (rpcpool.Client, error) {
		solanaClient := rpc.New(url)
		
		return &svmClientAdapter{
			client: solanaClient,
		}, nil
	}
}

// CreateSVMHealthChecker creates a health checker for SVM endpoints
func CreateSVMHealthChecker(expectedGenesisHash string) rpcpool.HealthChecker {
	return &svmHealthChecker{
		expectedGenesisHash: expectedGenesisHash,
	}
}

// svmHealthChecker implements rpcpool.HealthChecker for SVM endpoints
type svmHealthChecker struct {
	expectedGenesisHash string
}

// CheckHealth performs comprehensive health checks on an SVM client
func (h *svmHealthChecker) CheckHealth(ctx context.Context, client rpcpool.Client) error {
	// Cast to our SVM adapter
	svmAdapter, ok := client.(*svmClientAdapter)
	if !ok {
		return fmt.Errorf("invalid client type for SVM health check: %T", client)
	}

	rpcClient := svmAdapter.GetSolanaClient()

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

// GetSolanaClientFromPool extracts the rpc.Client from a pool endpoint
func GetSolanaClientFromPool(endpoint *rpcpool.Endpoint) (*rpc.Client, error) {
	client := endpoint.GetClient()
	if client == nil {
		return nil, fmt.Errorf("no client available for endpoint %s", endpoint.URL)
	}

	svmAdapter, ok := client.(*svmClientAdapter)
	if !ok {
		return nil, fmt.Errorf("invalid client type: expected svmClientAdapter, got %T", client)
	}

	return svmAdapter.GetSolanaClient(), nil
}