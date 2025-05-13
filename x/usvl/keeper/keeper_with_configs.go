package keeper

import (
	"context"
	"fmt"

	"github.com/push-protocol/push-chain/x/usvl/types"
)

// KeeperWithConfigs is a special version of Keeper for testing
// that directly contains the chain configurations, bypassing the need
// for database/ORM access
type KeeperWithConfigs struct {
	ChainConfigs map[string]types.ChainConfigData
}

// VerifyExternalTransactionDirect verifies a transaction on an external chain
// using direct access to the chain configurations map
func (k *KeeperWithConfigs) VerifyExternalTransactionDirect(ctx context.Context, txHash string, caipAddress string) (*TransactionVerificationResult, error) {
	// Parse CAIP address
	caip, err := types.ParseCAIPAddress(caipAddress)
	if err != nil {
		return nil, fmt.Errorf("failed to parse CAIP address: %w", err)
	}

	// Get chain ID from CAIP format
	chainIdentifier := caip.GetChainIdentifier()

	// Find matching chain config directly from the map
	config, found := k.ChainConfigs[chainIdentifier]
	if !found {
		return nil, fmt.Errorf("no chain configuration found for CAIP prefix %s", chainIdentifier)
	}

	// Create a regular keeper to leverage the existing verification logic
	regularKeeper := Keeper{}

	// For now, only support Ethereum Sepolia verification
	// Check if this is a Sepolia chain
	// TODO: Remove it when new chains are addded
	if config.CaipPrefix != "eip155:11155111" {
		return nil, fmt.Errorf("only Ethereum Sepolia (eip155:11155111) verification is supported")
	}

	// Verify the EVM transaction
	return regularKeeper.verifyEVMTransaction(ctx, config, txHash, caip.Address)
}
