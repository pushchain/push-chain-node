package core

import (
	"context"
	"fmt"
	"time"

	"github.com/pushchain/push-chain-node/universalClient/api"
	"github.com/pushchain/push-chain-node/universalClient/chains"
	"github.com/pushchain/push-chain-node/universalClient/chains/common"
	"github.com/pushchain/push-chain-node/universalClient/config"
	"github.com/pushchain/push-chain-node/universalClient/db"
	"github.com/pushchain/push-chain-node/universalClient/keys"
	"github.com/pushchain/push-chain-node/universalClient/registry"
	"github.com/pushchain/push-chain-node/universalClient/store"
	uregistrytypes "github.com/pushchain/push-chain-node/x/uregistry/types"
	"github.com/rs/zerolog"
)

type UniversalClient struct {
	ctx       context.Context
	log       zerolog.Logger
	dbManager *db.ChainDBManager

	// Registry components
	registryClient *registry.RegistryClient
	configCache    *registry.ConfigCache
	configUpdater  *ConfigUpdater
	chainRegistry  *chains.ChainRegistry
	config         *config.Config
	queryServer    *api.Server

	// Hot key components
	keys         keys.UniversalValidatorKeys
	voteHandlers map[string]*VoteHandler // Per-chain vote handlers (chainID -> VoteHandler)
	keyMonitor   *KeyMonitor             // Monitors keys and permissions dynamically
	// Transaction cleanup
	transactionCleaner *db.PerChainTransactionCleaner
	// Gas price fetcher
	gasPriceFetcher *GasPriceFetcher
}

func NewUniversalClient(ctx context.Context, log zerolog.Logger, dbManager *db.ChainDBManager, cfg *config.Config) (*UniversalClient, error) {
	// Validate config
	if cfg == nil {
		return nil, fmt.Errorf("config is nil")
	}

	// PushChainGRPCURLs is a hard requirement
	if len(cfg.PushChainGRPCURLs) == 0 {
		return nil, fmt.Errorf("PushChainGRPCURLs is required but not configured")
	}

	// Create registry client
	registryClient, err := registry.NewRegistryClient(cfg.PushChainGRPCURLs, log)
	if err != nil {
		return nil, fmt.Errorf("failed to create registry client: %w", err)
	}

	// Create config cache
	configCache := registry.NewConfigCache(log)

	// Create chain registry
	chainRegistry := chains.NewChainRegistry(dbManager, log)
	chainRegistry.SetAppConfig(cfg)

	// Create config updater
	configUpdater := NewConfigUpdater(
		registryClient,
		configCache,
		chainRegistry,
		cfg,
		log,
	)

	// Create per-chain transaction cleaner
	transactionCleaner := db.NewPerChainTransactionCleaner(
		dbManager,
		cfg,
		log,
	)

	// Create gas price fetcher
	gasPriceFetcher := NewGasPriceFetcher(
		chainRegistry,
		cfg,
		log,
	)

	// Create the client
	uc := &UniversalClient{
		ctx:                ctx,
		log:                log,
		dbManager:          dbManager,
		registryClient:     registryClient,
		configCache:        configCache,
		configUpdater:      configUpdater,
		chainRegistry:      chainRegistry,
		config:             cfg,
		voteHandlers:       make(map[string]*VoteHandler),
		transactionCleaner: transactionCleaner,
		gasPriceFetcher:    gasPriceFetcher,
	}

	// Create key monitor for dynamic key detection
	// PushChainGRPCURLs is guaranteed to exist at this point
	keyCheckInterval := 30 * time.Second // Default to 30 seconds
	if cfg.KeyCheckInterval > 0 {
		keyCheckInterval = time.Duration(cfg.KeyCheckInterval) * time.Second
	}

	uc.keyMonitor = NewKeyMonitor(
		ctx,
		log,
		cfg,
		cfg.PushChainGRPCURLs[0],
		keyCheckInterval,
	)

	// Set callbacks for when keys change
	uc.keyMonitor.SetCallbacks(
		func(keys keys.UniversalValidatorKeys) {
			uc.keys = keys
			// Create per-chain vote handlers
			uc.createVoteHandlersForAllChains(keys)

			// Process any transactions awaiting votes
			go uc.processAwaitingVoteTransactions()
		},
		func() {
			uc.keys = nil
			uc.voteHandlers = make(map[string]*VoteHandler)
			uc.chainRegistry.SetVoteHandlers(nil)
		},
	)

	// Create query server
	log.Info().Int("port", cfg.QueryServerPort).Msg("Creating query server")
	uc.queryServer = api.NewServer(uc, log, cfg.QueryServerPort)

	return uc, nil
}

func (uc *UniversalClient) Start() error {
	uc.log.Info().Msg("🚀 Starting universal client...")

	// Start key monitor for dynamic key detection and voting
	if uc.keyMonitor != nil {
		if err := uc.keyMonitor.Start(); err != nil {
			uc.log.Error().
				Err(err).
				Msg("Failed to start key monitor - voting will be disabled")
		}
	}

	// Check for required components
	if uc.configUpdater == nil {
		return fmt.Errorf("config updater is not initialized")
	}

	// Start the config updater
	if err := uc.configUpdater.Start(uc.ctx); err != nil {
		return fmt.Errorf("failed to start config updater: %w", err)
	}

	// Start the transaction cleaner
	if uc.transactionCleaner != nil {
		if err := uc.transactionCleaner.Start(uc.ctx); err != nil {
			return fmt.Errorf("failed to start transaction cleaner: %w", err)
		}
	}

	// Start the gas price fetcher
	if uc.gasPriceFetcher != nil {
		if err := uc.gasPriceFetcher.Start(uc.ctx); err != nil {
			uc.log.Error().
				Err(err).
				Msg("Failed to start gas price fetcher - gas prices will not be tracked")
			// Don't fail startup, just log the error
		}
	}

	// Start the query server
	if uc.queryServer != nil {
		uc.log.Info().Int("port", uc.config.QueryServerPort).Msg("Starting query server...")
		if err := uc.queryServer.Start(); err != nil {
			return fmt.Errorf("failed to start query server: %w", err)
		}
	} else {
		uc.log.Warn().Msg("Query server is nil, skipping start")
	}

	uc.log.Info().Msg("✅ Initialization complete. Entering main loop...")

	<-uc.ctx.Done()

	uc.log.Info().Msg("🛑 Shutting down universal client...")

	// Stop query server
	if err := uc.queryServer.Stop(); err != nil {
		uc.log.Error().Err(err).Msg("error stopping query server")
	}

	// Stop key monitor
	if uc.keyMonitor != nil {
		uc.keyMonitor.Stop()
	}

	// Stop config updater
	uc.configUpdater.Stop()

	// Stop transaction cleaner
	uc.transactionCleaner.Stop()

	// Stop gas price fetcher
	if uc.gasPriceFetcher != nil {
		uc.gasPriceFetcher.Stop()
	}

	// Stop all chain clients
	uc.chainRegistry.StopAll()

	// Close registry client connection
	if err := uc.registryClient.Close(); err != nil {
		uc.log.Error().Err(err).Msg("error closing registry client")
	}

	// Close all database connections
	if err := uc.dbManager.CloseAll(); err != nil {
		uc.log.Error().Err(err).Msg("error closing database connections")
		return err
	}

	return nil
}

// GetChainConfig returns the cached configuration for a specific chain
func (uc *UniversalClient) GetChainConfig(chainID string) *uregistrytypes.ChainConfig {
	return uc.configCache.GetChainConfig(chainID)
}

// GetAllChainConfigs returns all cached chain configurations
func (uc *UniversalClient) GetAllChainConfigs() []*uregistrytypes.ChainConfig {
	return uc.configCache.GetAllChainConfigs()
}

// GetTokenConfig returns the cached configuration for a specific token
func (uc *UniversalClient) GetTokenConfig(chain, address string) *uregistrytypes.TokenConfig {
	return uc.configCache.GetTokenConfig(chain, address)
}

// GetAllTokenConfigs returns all cached token configurations
func (uc *UniversalClient) GetAllTokenConfigs() []*uregistrytypes.TokenConfig {
	return uc.configCache.GetAllTokenConfigs()
}

// GetTokenConfigsByChain returns all cached token configurations for a specific chain
func (uc *UniversalClient) GetTokenConfigsByChain(chain string) []*uregistrytypes.TokenConfig {
	return uc.configCache.GetTokenConfigsByChain(chain)
}

// GetCacheLastUpdate returns the last update timestamp of the cache
func (uc *UniversalClient) GetCacheLastUpdate() time.Time {
	return uc.configCache.GetLastUpdate()
}

// GetChainClient returns the chain client for a specific chain
func (uc *UniversalClient) GetChainClient(chainID string) common.ChainClient {
	return uc.chainRegistry.GetChain(chainID)
}

// ForceConfigUpdate triggers an immediate configuration update
func (uc *UniversalClient) ForceConfigUpdate() error {
	if uc.configUpdater == nil {
		return fmt.Errorf("config updater is not initialized")
	}
	return uc.configUpdater.ForceUpdate(uc.ctx)
}

// GetVoteHandler returns the vote handler for a specific chain (may be nil if keys not configured)
func (uc *UniversalClient) GetVoteHandler(chainID string) *VoteHandler {
	return uc.voteHandlers[chainID]
}

// GetAllVoteHandlers returns all vote handlers
func (uc *UniversalClient) GetAllVoteHandlers() map[string]*VoteHandler {
	return uc.voteHandlers
}

// createVoteHandlersForAllChains creates vote handlers for all existing chain databases
func (uc *UniversalClient) createVoteHandlersForAllChains(keys keys.UniversalValidatorKeys) {
	if uc.keyMonitor == nil {
		uc.log.Error().Msg("Key monitor is nil, cannot create vote handlers")
		return
	}

	chainDatabases := uc.dbManager.GetAllDatabases()

	// Get granter from key monitor (assuming it's stored in the keys or accessible somehow)
	// For now, we'll need to get this from the key monitor
	granter := uc.keyMonitor.GetCurrentGranter()
	if granter == "" {
		uc.log.Error().Msg("No granter found, cannot create vote handlers")
		return
	}

	voteHandlers := make(map[string]*VoteHandler)
	for chainID, db := range chainDatabases {
		// Skip the universal validator database as it doesn't store chain transactions
		if chainID == "universal-validator" {
			continue
		}

		// Create TxSigner (we'll need to extract this from key monitor or create interface)
		txSigner := uc.keyMonitor.GetCurrentTxSigner()
		if txSigner == nil {
			uc.log.Error().
				Str("chain_id", chainID).
				Msg("No tx signer available, cannot create vote handler for chain")
			continue
		}

		// Create vote handler for this chain
		voteHandler := NewVoteHandler(
			txSigner,
			db,
			uc.log,
			keys,
			granter,
		)

		voteHandlers[chainID] = voteHandler

		uc.log.Info().
			Str("chain_id", chainID).
			Msg("Created vote handler for chain")
	}

	uc.voteHandlers = voteHandlers

	// Update chain registry with the new vote handlers (convert to interface type)
	interfaceHandlers := make(map[string]common.VoteHandler)
	for chainID, handler := range voteHandlers {
		interfaceHandlers[chainID] = handler
	}
	uc.chainRegistry.SetVoteHandlers(interfaceHandlers)

	uc.log.Info().
		Int("vote_handlers", len(voteHandlers)).
		Msg("Successfully created per-chain vote handlers")
}

// processAwaitingVoteTransactions processes transactions that are awaiting votes
func (uc *UniversalClient) processAwaitingVoteTransactions() {
	// Get all chain databases to check for awaiting vote transactions
	chainDatabases := uc.dbManager.GetAllDatabases()

	if len(chainDatabases) == 0 {
		uc.log.Debug().Msg("no chain databases available for processing awaiting vote transactions")
		return
	}

	// Structure to track transactions with their chain context
	type chainTransaction struct {
		store.ChainTransaction
		ChainID string
	}

	var allAwaitingTxsWithChain []chainTransaction
	totalCount := 0

	// Query each chain database for awaiting vote transactions
	for chainID, db := range chainDatabases {
		// Skip the universal validator database as it doesn't store chain transactions
		if chainID == "universal-validator" {
			continue
		}

		var awaitingTxs []store.ChainTransaction
		err := db.Client().
			Where("status = ?", "awaiting_vote").
			Find(&awaitingTxs).Error

		if err != nil {
			uc.log.Error().
				Err(err).
				Str("chain_id", chainID).
				Msg("failed to fetch transactions awaiting votes from chain database")
			continue
		}

		if len(awaitingTxs) > 0 {
			uc.log.Debug().
				Str("chain_id", chainID).
				Int("count", len(awaitingTxs)).
				Msg("found transactions awaiting votes in chain database")

			// Add chain context to transactions
			for _, tx := range awaitingTxs {
				allAwaitingTxsWithChain = append(allAwaitingTxsWithChain, chainTransaction{
					ChainTransaction: tx,
					ChainID:          chainID,
				})
			}

			totalCount += len(awaitingTxs)
		}
	}

	if totalCount == 0 {
		uc.log.Debug().Msg("no transactions awaiting votes found across all chain databases")
		return
	}

	uc.log.Info().
		Int("count", totalCount).
		Int("chains_checked", len(chainDatabases)-1). // -1 to exclude universal-validator
		Msg("processing backlog of transactions awaiting votes")

	// Vote on each transaction using the appropriate chain's vote handler
	ctx := context.Background()
	successCount := 0
	for _, txWithChain := range allAwaitingTxsWithChain {
		// Get the vote handler for this chain
		voteHandler, exists := uc.voteHandlers[txWithChain.ChainID]
		if !exists || voteHandler == nil {
			uc.log.Error().
				Str("tx_hash", txWithChain.TxHash).
				Str("chain_id", txWithChain.ChainID).
				Msg("no vote handler available for chain - skipping transaction")
			continue
		}

		// Make a copy to pass by reference
		txCopy := txWithChain.ChainTransaction
		if err := voteHandler.VoteAndConfirm(ctx, &txCopy); err != nil {
			uc.log.Error().
				Str("tx_hash", txWithChain.TxHash).
				Str("chain_id", txWithChain.ChainID).
				Err(err).
				Msg("failed to vote on backlog transaction")
			// Transaction remains in awaiting_vote for next retry
		} else {
			uc.log.Debug().
				Str("tx_hash", txWithChain.TxHash).
				Str("chain_id", txWithChain.ChainID).
				Msg("successfully voted on backlog transaction")
			successCount++
		}
	}

	uc.log.Info().
		Int("processed", successCount).
		Int("total", totalCount).
		Msg("completed processing backlog transactions")
}
