package chains

import (
	"context"
	"fmt"
	"sync"

	"github.com/rs/zerolog"

	"github.com/pushchain/push-chain-node/universalClient/chains/common"
	"github.com/pushchain/push-chain-node/universalClient/chains/evm"
	"github.com/pushchain/push-chain-node/universalClient/chains/svm"
	"github.com/pushchain/push-chain-node/universalClient/config"
	"github.com/pushchain/push-chain-node/universalClient/db"
	uregistrytypes "github.com/pushchain/push-chain-node/x/uregistry/types"
)

// ChainRegistryObserver is an interface for observing chain addition events
type ChainRegistryObserver interface {
	OnChainAdded(chainID string)
}

// ChainRegistry manages chain clients based on their configurations
type ChainRegistry struct {
	mu           sync.RWMutex
	chains       map[string]common.ChainClient // key: CAIP-2 chain ID
	logger       zerolog.Logger
	dbManager    *db.ChainDBManager
	appConfig    *config.Config
	voteHandlers map[string]common.VoteHandler // Per-chain vote handlers (chainID -> VoteHandler)
	observer     ChainRegistryObserver        // Observer for chain events
}

// NewChainRegistry creates a new chain registry
func NewChainRegistry(dbManager *db.ChainDBManager, logger zerolog.Logger) *ChainRegistry {
	return &ChainRegistry{
		chains:       make(map[string]common.ChainClient),
		voteHandlers: make(map[string]common.VoteHandler),
		logger:       logger.With().Str("component", "chain_registry").Logger(),
		dbManager:    dbManager,
	}
}

// SetAppConfig sets the application config for the registry
func (r *ChainRegistry) SetAppConfig(cfg *config.Config) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.appConfig = cfg
}

// SetObserver sets the observer for chain events
func (r *ChainRegistry) SetObserver(observer ChainRegistryObserver) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.observer = observer
}

// SetVoteHandlers sets per-chain vote handlers
func (r *ChainRegistry) SetVoteHandlers(handlers map[string]common.VoteHandler) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if handlers == nil {
		r.voteHandlers = make(map[string]common.VoteHandler)
	} else {
		r.voteHandlers = handlers
	}

	// Set vote handler on all existing chains
	for chainID, client := range r.chains {
		if handler, exists := r.voteHandlers[chainID]; exists && handler != nil {
			client.SetVoteHandler(handler)
			r.logger.Info().
				Str("chain", chainID).
				Msg("vote handler set on existing chain")
		} else {
			client.SetVoteHandler(nil)
			r.logger.Warn().
				Str("chain", chainID).
				Msg("no vote handler available for chain")
		}
	}
}

// SetVoteHandler sets the vote handler for all chains (backward compatibility)
func (r *ChainRegistry) SetVoteHandler(handler common.VoteHandler) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Set the same handler for all chains
	for chainID := range r.chains {
		r.voteHandlers[chainID] = handler
	}

	// Set vote handler on all existing chains
	for chainID, client := range r.chains {
		client.SetVoteHandler(handler)
		r.logger.Info().
			Str("chain", chainID).
			Msg("vote handler set on existing chain")
	}
}

// CreateChainClient creates a chain client based on VM type
func (r *ChainRegistry) CreateChainClient(config *uregistrytypes.ChainConfig) (common.ChainClient, error) {
	if config == nil {
		return nil, fmt.Errorf("chain config is nil")
	}

	r.logger.Debug().
		Str("chain", config.Chain).
		Int32("vm_type", int32(config.VmType)).
		Msg("creating chain client")

	// Get chain-specific database
	chainDB, err := r.dbManager.GetChainDB(config.Chain)
	if err != nil {
		return nil, fmt.Errorf("failed to get database for chain %s: %w", config.Chain, err)
	}

	switch config.VmType {
	case uregistrytypes.VmType_EVM:
		return evm.NewClient(config, chainDB, r.appConfig, r.logger)
	case uregistrytypes.VmType_SVM:
		return svm.NewClient(config, chainDB, r.appConfig, r.logger)
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

	// Set vote handler if available for this chain
	if handler, exists := r.voteHandlers[chainID]; exists && handler != nil {
		client.SetVoteHandler(handler)
		r.logger.Info().
			Str("chain", chainID).
			Msg("vote handler set for chain")
	} else {
		r.logger.Warn().
			Str("chain", chainID).
			Msg("no vote handler available for chain - chain will not vote on transactions")
	}

	// Start the chain client
	if err := client.Start(ctx); err != nil {
		return fmt.Errorf("failed to start chain client for %s: %w", chainID, err)
	}

	// Store the client first
	wasNew := !exists
	r.chains[chainID] = client
	r.logger.Info().
		Str("chain", chainID).
		Msg("successfully added/updated chain client")

	// Notify observer if this was a new chain addition OR if no vote handler exists
	// This handles the case where chains are added after keys are detected
	shouldNotify := wasNew
	if !shouldNotify && r.observer != nil {
		// Check if vote handler exists for this chain
		if handler, exists := r.voteHandlers[chainID]; !exists || handler == nil {
			shouldNotify = true
			r.logger.Debug().
				Str("chain", chainID).
				Msg("chain has no vote handler, will notify observer")
		}
	}
	
	if shouldNotify && r.observer != nil {
		r.logger.Debug().
			Str("chain", chainID).
			Msg("notifying observer of chain addition")
		// Call observer in a goroutine to avoid holding the lock
		go r.observer.OnChainAdded(chainID)
	}

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
func (r *ChainRegistry) GetChain(chainID string) common.ChainClient {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.chains[chainID]
}

// GetAllChains returns all registered chain clients
func (r *ChainRegistry) GetAllChains() map[string]common.ChainClient {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Create a copy to avoid race conditions
	chains := make(map[string]common.ChainClient)
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
	r.chains = make(map[string]common.ChainClient)
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

// GetDatabaseStats returns statistics about all managed databases
func (r *ChainRegistry) GetDatabaseStats() map[string]interface{} {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.dbManager.GetDatabaseStats()
}

// configsEqual compares two chain configurations
func configsEqual(a, b *uregistrytypes.ChainConfig) bool {
	if a == nil || b == nil {
		return a == b
	}

	// Handle Enabled field comparison
	enabledEqual := false
	if a.Enabled == nil && b.Enabled == nil {
		enabledEqual = true
	} else if a.Enabled != nil && b.Enabled != nil {
		enabledEqual = a.Enabled.IsInboundEnabled == b.Enabled.IsInboundEnabled &&
			a.Enabled.IsOutboundEnabled == b.Enabled.IsOutboundEnabled
	}

	// Compare relevant fields
	return a.Chain == b.Chain &&
		a.VmType == b.VmType &&
		a.GatewayAddress == b.GatewayAddress &&
		enabledEqual
}
