package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/log"
	"github.com/push-protocol/push-chain/utils/env"
	"github.com/push-protocol/push-chain/x/usvl/types"
)

// NoOpLogger is a logger that does nothing, used for testing
type NoOpLogger struct{}

func (l NoOpLogger) Debug(msg string, keyVals ...interface{}) {}
func (l NoOpLogger) Info(msg string, keyVals ...interface{})  {}
func (l NoOpLogger) Error(msg string, keyVals ...interface{}) {}
// func (l NoOpLogger) With(keyVals ...interface{}) log.Logger   { return l }

// NewNoOpLogger creates a new no-op logger for testing

// KeeperWithConfigs is a special version of Keeper for testing
// that directly contains the chain configurations, bypassing the need
// for database/ORM access
type KeeperWithConfigs struct {
	ChainConfigs map[string]types.ChainConfigData
	configCache  *ConfigCache // In-memory cache for chain configurations
	logger       log.Logger
}

// NewKeeperWithConfigs creates a new KeeperWithConfigs instance with initialized cache
func NewKeeperWithConfigs(configs map[string]types.ChainConfigData) *KeeperWithConfigs {
	cache := NewConfigCache()

	// Populate the cache with initial configurations
	for _, config := range configs {
		cache.Set(config.ChainId, config)
	}

	return &KeeperWithConfigs{
		ChainConfigs: configs,
		configCache:  cache,
		// logger:       NewNoOpLogger(),
	}
}

// VerifyExternalTransactionDirect verifies a transaction on an external chain
// using direct access to the chain configurations map and in-memory cache
func (k *KeeperWithConfigs) VerifyExternalTransactionDirect(ctx context.Context, txHash string, caipAddress string) (*TransactionVerificationResult, error) {
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

	// Check for environment variable override for RPC URL
	if customRPC, found := env.GetRpcUrlOverride(config.ChainId); found {
		// Log if logger is available
		if k.logger != nil {
			k.logger.Info("Using custom RPC URL from environment variable",
				"chain_id", config.ChainId,
				"original_rpc", config.PublicRpcUrl,
				"custom_rpc", customRPC)
		}

		// Create a copy of the config with the overridden RPC URL
		configCopy := config
		configCopy.PublicRpcUrl = customRPC
		config = configCopy
	}

	// Create a regular keeper with a logger to avoid nil pointer dereference
	regularKeeper := Keeper{
		logger:      k.logger,
		configCache: k.configCache, // Share the same cache
	}

	// Determine which verification method to use based on the VM type
	switch config.VmType {
	case types.VmTypeEvm:
		return regularKeeper.verifyEVMTransaction(ctx, config, txHash, caip.Address)
	case types.VmTypeWasm:
		return nil, fmt.Errorf("CosmWasm transaction verification not yet implemented")
	case types.VmTypeSvm:
		return nil, fmt.Errorf("Solana VM transaction verification not yet implemented")
	default:
		return nil, fmt.Errorf("unsupported VM type: %d", config.VmType)
	}
}
