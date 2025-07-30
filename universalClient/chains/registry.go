package chains

import (
	"context"
	"fmt"
	"sync"

	"github.com/rs/zerolog"

	"github.com/rollchains/pchain/universalClient/chains/base"
	"github.com/rollchains/pchain/universalClient/chains/evm"
	"github.com/rollchains/pchain/universalClient/chains/solana"
	uregistrytypes "github.com/rollchains/pchain/x/uregistry/types"
)

// ChainRegistry manages chain clients based on their configurations
type ChainRegistry struct {
	mu     sync.RWMutex
	chains map[string]base.ChainClient // key: CAIP-2 chain ID
	logger zerolog.Logger
}

// NewChainRegistry creates a new chain registry
func NewChainRegistry(logger zerolog.Logger) *ChainRegistry {
	return &ChainRegistry{
		chains: make(map[string]base.ChainClient),
		logger: logger.With().Str("component", "chain_registry").Logger(),
	}
}

// CreateChainClient creates a chain client based on VM type
func (r *ChainRegistry) CreateChainClient(config *uregistrytypes.ChainConfig) (base.ChainClient, error) {
	if config == nil {
		return nil, fmt.Errorf("chain config is nil")
	}

	r.logger.Debug().
		Str("chain", config.Chain).
		Int32("vm_type", int32(config.VmType)).
		Msg("creating chain client")

	switch config.VmType {
	case uregistrytypes.VmType_EVM:
		return evm.NewClient(config, r.logger)
	case uregistrytypes.VmType_SVM: // SVM in the enum
		return solana.NewClient(config, r.logger) // But use solana package
	default:
		return nil, fmt.Errorf("unsupported VM type: %v", config.VmType)
	}
}

// AddOrUpdateChain adds a new chain or updates an existing one
func (r *ChainRegistry) AddOrUpdateChain(ctx context.Context, config *uregistrytypes.ChainConfig) error {
	if config == nil || config.Chain == "" {
		return fmt.Errorf("invalid chain config")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	chainID := config.Chain

	// Check if chain already exists
	existingClient, exists := r.chains[chainID]
	if exists {
		// Check if configuration has changed
		existingConfig := existingClient.GetConfig()
		if configsEqual(existingConfig, config) {
			r.logger.Debug().
				Str("chain", chainID).
				Msg("chain config unchanged, skipping update")
			return nil
		}

		// Stop the existing client
		r.logger.Info().
			Str("chain", chainID).
			Msg("stopping existing chain client for update")
		if err := existingClient.Stop(); err != nil {
			r.logger.Error().
				Err(err).
				Str("chain", chainID).
				Msg("failed to stop existing chain client")
		}
		delete(r.chains, chainID)
	}

	// Create new chain client
	client, err := r.CreateChainClient(config)
	if err != nil {
		return fmt.Errorf("failed to create chain client for %s: %w", chainID, err)
	}

	// Start the chain client
	if err := client.Start(ctx); err != nil {
		return fmt.Errorf("failed to start chain client for %s: %w", chainID, err)
	}

	r.chains[chainID] = client
	r.logger.Info().
		Str("chain", chainID).
		Msg("successfully added/updated chain client")

	return nil
}

// RemoveChain removes a chain from the registry
func (r *ChainRegistry) RemoveChain(chainID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	client, exists := r.chains[chainID]
	if !exists {
		return
	}

	r.logger.Info().
		Str("chain", chainID).
		Msg("removing chain client")

	// Stop the client
	if err := client.Stop(); err != nil {
		r.logger.Error().
			Err(err).
			Str("chain", chainID).
			Msg("error stopping chain client during removal")
	}

	delete(r.chains, chainID)
}

// GetChain retrieves a chain client by ID
func (r *ChainRegistry) GetChain(chainID string) base.ChainClient {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.chains[chainID]
}

// GetAllChains returns all registered chain clients
func (r *ChainRegistry) GetAllChains() map[string]base.ChainClient {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Create a copy to avoid race conditions
	chains := make(map[string]base.ChainClient)
	for k, v := range r.chains {
		chains[k] = v
	}

	return chains
}

// StopAll stops all chain clients
func (r *ChainRegistry) StopAll() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.logger.Info().Msg("stopping all chain clients")

	for chainID, client := range r.chains {
		if err := client.Stop(); err != nil {
			r.logger.Error().
				Err(err).
				Str("chain", chainID).
				Msg("error stopping chain client")
		}
	}

	// Clear the registry
	r.chains = make(map[string]base.ChainClient)
}

// GetHealthStatus returns the health status of all chains
func (r *ChainRegistry) GetHealthStatus() map[string]bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	status := make(map[string]bool)
	for chainID, client := range r.chains {
		status[chainID] = client.IsHealthy()
	}

	return status
}

// configsEqual compares two chain configurations
func configsEqual(a, b *uregistrytypes.ChainConfig) bool {
	if a == nil || b == nil {
		return a == b
	}

	// Compare relevant fields
	return a.Chain == b.Chain &&
		a.VmType == b.VmType &&
		a.PublicRpcUrl == b.PublicRpcUrl &&
		a.GatewayAddress == b.GatewayAddress &&
		a.Enabled == b.Enabled
}