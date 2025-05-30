package keeper

import (
	"context"
	"fmt"

	"github.com/push-protocol/push-chain/x/utv/types"
)

// VerifyExternalTransactionToLockerDirect is a direct access method for KeeperWithConfigs
// to verify a transaction is directed to the locker contract.
// This is needed for integration tests with real transactions.
func (k *KeeperWithConfigs) VerifyExternalTransactionToLockerDirect(ctx context.Context, txHash string, caipAddress string) (*TransactionVerificationResult, error) {
	// Parse CAIP address
	caip, err := types.ParseCAIPAddress(caipAddress)
	if err != nil {
		return nil, fmt.Errorf("failed to parse CAIP address: %w", err)
	}

	// Get chain ID from CAIP format
	chainIdentifier := caip.GetChainIdentifier()

	// Try to get the config from the cache first if it's available
	var config types.ChainConfigData
	var found bool

	if k.configCache != nil {
		config, found = k.configCache.GetByCaipPrefix(chainIdentifier)
	}

	// If not in cache or cache not initialized, look in the direct map
	if !found {
		config, found = k.ChainConfigs[chainIdentifier]
		if !found {
			return nil, fmt.Errorf("no chain configuration found for CAIP prefix %s", chainIdentifier)
		}

		// Add to cache if available
		if k.configCache != nil {
			k.configCache.Set(config.ChainId, config)
		}
	}

	// Create a regular keeper with necessary components
	regularKeeper := Keeper{
		logger:      k.logger,
		configCache: k.configCache, // Share the same cache
	}

	// Use the keeper method for verifying transactions to locker
	switch config.VmType {
	case types.VmTypeEvm:
		return regularKeeper.verifyEVMTransactionToLocker(ctx, config, txHash, caip.Address)
	default:
		return nil, fmt.Errorf("unsupported VM type: %d", config.VmType)
	}
}
