package core

import (
	"context"
	"fmt"
	"time"

	"github.com/pushchain/push-chain-node/universalClient/api"
	"github.com/pushchain/push-chain-node/universalClient/cache"
	"github.com/pushchain/push-chain-node/universalClient/chains"
	"github.com/pushchain/push-chain-node/universalClient/chains/common"
	"github.com/pushchain/push-chain-node/universalClient/config"
	"github.com/pushchain/push-chain-node/universalClient/cron"
	"github.com/pushchain/push-chain-node/universalClient/db"
	"github.com/pushchain/push-chain-node/universalClient/keys"
	"github.com/pushchain/push-chain-node/universalClient/pushcore"
	"github.com/rs/zerolog"
)

type UniversalClient struct {
	ctx       context.Context
	log       zerolog.Logger
	dbManager *db.ChainDBManager

	// Registry components
	chainRegistry *chains.ChainRegistry
	config        *config.Config
	queryServer   *api.Server

	// Hot key components
	keys                     keys.UniversalValidatorKeys
	voteHandlers             map[string]*VoteHandler // Per-chain vote handlers (chainID -> VoteHandler)
	pendingVoteHandlerChains map[string]bool         // Chains waiting for vote handler creation
	keyMonitor               *KeyMonitor             // Monitors keys and permissions dynamically
	// Transaction cleanup
	transactionCleaner *db.PerChainTransactionCleaner
	// Gas price fetcher
	gasPriceFetcher *GasPriceFetcher

	pushCore         *pushcore.Client
	cache            *cache.Cache
	chainCacheJob    *cron.ChainCacheJob
	chainRegistryJob *cron.ChainRegistryJob
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

	// Create chain registry
	chainRegistry := chains.NewChainRegistry(dbManager, log)
	chainRegistry.SetAppConfig(cfg)

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

	// New pushcore + cache + cron job
	pushCore, err := pushcore.New(cfg.PushChainGRPCURLs, log)
	if err != nil {
		return nil, fmt.Errorf("failed to create pushcore client: %w", err)
	}
	cache := cache.New(log)

	// refresh every minute; 8s per-sync timeout
	chainCacheJob := cron.NewChainCacheJob(cache, pushCore, time.Duration(cfg.ConfigRefreshIntervalSeconds), 8*time.Second, log)

	chainRegistryJob := cron.NewChainRegistryJob(cache, chainRegistry, time.Duration(cfg.ConfigRefreshIntervalSeconds), 8*time.Second, log)

	// Create the client
	uc := &UniversalClient{
		ctx:       ctx,
		log:       log,
		dbManager: dbManager,

		// configUpdater:            configUpdater,
		chainRegistry:            chainRegistry,
		config:                   cfg,
		voteHandlers:             make(map[string]*VoteHandler),
		pendingVoteHandlerChains: make(map[string]bool),
		transactionCleaner:       transactionCleaner,
		gasPriceFetcher:          gasPriceFetcher,

		pushCore:         pushCore,
		cache:            cache,
		chainCacheJob:    chainCacheJob,
		chainRegistryJob: chainRegistryJob,
	}

	// Register as observer for chain addition events
	chainRegistry.SetObserver(uc)

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

	if uc.chainCacheJob != nil {
		if err := uc.chainCacheJob.Start(uc.ctx); err != nil {
			uc.log.Error().Err(err).Msg("failed to start chain cache cron")
		}
	}

	if uc.chainRegistry != nil {
		if err := uc.chainRegistryJob.Start(uc.ctx); err != nil {
			uc.log.Error().Err(err).Msg("failed to start chain registry cron")
		}
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

	// Stop transaction cleaner
	uc.transactionCleaner.Stop()

	// Stop gas price fetcher
	if uc.gasPriceFetcher != nil {
		uc.gasPriceFetcher.Stop()
	}

	// Stop all chain clients
	uc.chainRegistry.StopAll()

	// Close all database connections
	if err := uc.dbManager.CloseAll(); err != nil {
		uc.log.Error().Err(err).Msg("error closing database connections")
		return err
	}

	// Stop chain cache cron
	if uc.chainCacheJob != nil {
		uc.chainCacheJob.Stop()
	}

	// Stop chain registry cron
	if uc.chainRegistryJob != nil {
		uc.chainRegistryJob.Stop()
	}

	// Close pushcore client
	if uc.pushCore != nil {
		if err := uc.pushCore.Close(); err != nil {
			uc.log.Error().Err(err).Msg("error closing pushcore client")
		}
	}

	return nil
}

// GetAllChainConfigs returns all cached chain configurations
func (uc *UniversalClient) GetAllChainData() []*cache.ChainData {
	return uc.cache.GetAllChains()
}

// OnChainAdded implements ChainRegistryObserver interface
func (uc *UniversalClient) OnChainAdded(chainID string) {
	uc.log.Info().
		Str("chain_id", chainID).
		Msg("New chain added, checking if vote handler needed")
	uc.createVoteHandlerForChain(chainID)
}

// createVoteHandlerForChain creates a vote handler for a specific chain
func (uc *UniversalClient) createVoteHandlerForChain(chainID string) {
	// Check if we have valid keys
	if uc.keys == nil {
		uc.log.Debug().
			Str("chain_id", chainID).
			Msg("No valid keys available, cannot create vote handler for chain")
		return
	}

	// Skip the universal validator database
	if chainID == "universal-validator" {
		return
	}

	// Check if vote handler already exists
	if _, exists := uc.voteHandlers[chainID]; exists {
		uc.log.Debug().
			Str("chain_id", chainID).
			Msg("Vote handler already exists for chain")
		return
	}

	// Check if we have keys
	if uc.keys == nil {
		uc.log.Info().
			Str("chain_id", chainID).
			Msg("No keys available yet, adding chain to pending vote handler list")
		// Add to pending list
		if uc.pendingVoteHandlerChains == nil {
			uc.pendingVoteHandlerChains = make(map[string]bool)
		}
		uc.pendingVoteHandlerChains[chainID] = true
		return
	}

	// Get database for this chain
	db, err := uc.dbManager.GetChainDB(chainID)
	if err != nil {
		uc.log.Warn().
			Str("chain_id", chainID).
			Err(err).
			Msg("Failed to get database for chain, cannot create vote handler")
		return
	}

	// Get granter from key monitor
	granter := uc.keyMonitor.GetCurrentGranter()
	if granter == "" {
		uc.log.Info().
			Str("chain_id", chainID).
			Msg("No granter found yet, adding chain to pending vote handler list")
		// Add to pending list
		if uc.pendingVoteHandlerChains == nil {
			uc.pendingVoteHandlerChains = make(map[string]bool)
		}
		uc.pendingVoteHandlerChains[chainID] = true
		return
	}

	// Get TxSigner from key monitor
	txSigner := uc.keyMonitor.GetCurrentTxSigner()
	if txSigner == nil {
		uc.log.Info().
			Str("chain_id", chainID).
			Msg("No tx signer available yet, adding chain to pending vote handler list")
		// Add to pending list
		if uc.pendingVoteHandlerChains == nil {
			uc.pendingVoteHandlerChains = make(map[string]bool)
		}
		uc.pendingVoteHandlerChains[chainID] = true
		return
	}

	// Create vote handler for this chain
	voteHandler := NewVoteHandler(
		txSigner,
		db,
		uc.log,
		uc.keys,
		granter,
	)

	// Store the vote handler
	uc.voteHandlers[chainID] = voteHandler

	// Update chain registry with the new vote handler
	uc.chainRegistry.SetVoteHandlers(map[string]common.VoteHandler{
		chainID: voteHandler,
	})

	uc.log.Info().
		Str("chain_id", chainID).
		Msg("Created vote handler for newly added chain")
}

// createVoteHandlersForAllChains creates vote handlers for all existing chain databases
func (uc *UniversalClient) createVoteHandlersForAllChains(keys keys.UniversalValidatorKeys) {
	uc.log.Info().
		Msg("Starting vote handler creation for all chains")

	if uc.keyMonitor == nil {
		uc.log.Error().Msg("Key monitor is nil, cannot create vote handlers")
		return
	}

	chainDatabases := uc.dbManager.GetAllDatabases()

	uc.log.Info().
		Int("database_count", len(chainDatabases)).
		Interface("database_chains", func() []string {
			var chains []string
			for chainID := range chainDatabases {
				chains = append(chains, chainID)
			}
			return chains
		}()).
		Msg("Found chain databases")

	// Check if we have any chain databases
	if len(chainDatabases) == 0 {
		uc.log.Warn().
			Msg("No chain databases exist yet - vote handlers will be created when chains are added")
		// Store the keys for later use when chains are added
		uc.keys = keys
		return
	}

	uc.log.Info().Msg("About to retrieve granter from key monitor")

	// Get granter from key monitor
	granter := uc.keyMonitor.GetCurrentGranter()
	uc.log.Info().
		Str("granter", granter).
		Msg("Retrieved granter from key monitor")

	if granter == "" {
		uc.log.Error().Msg("No granter found, cannot create vote handlers")
		return
	}

	// Get TxSigner from key monitor first (outside loop for efficiency)
	txSigner := uc.keyMonitor.GetCurrentTxSigner()
	uc.log.Info().
		Bool("tx_signer_available", txSigner != nil).
		Msg("Retrieved TxSigner from key monitor")

	if txSigner == nil {
		uc.log.Error().Msg("No TxSigner available, cannot create any vote handlers")
		return
	}

	voteHandlers := make(map[string]*VoteHandler)
	processedChains := 0
	skippedChains := 0

	for chainID, db := range chainDatabases {
		// Skip the universal validator database as it doesn't store chain transactions
		if chainID == "universal-validator" {
			uc.log.Debug().
				Str("chain_id", chainID).
				Msg("Skipping universal validator database")
			skippedChains++
			continue
		}

		uc.log.Info().
			Str("chain_id", chainID).
			Msg("Creating vote handler for chain")

		// Create vote handler for this chain with error recovery
		func() {
			defer func() {
				if r := recover(); r != nil {
					uc.log.Error().
						Str("chain_id", chainID).
						Interface("panic", r).
						Msg("Panic recovered during vote handler creation")
				}
			}()

			voteHandler := NewVoteHandler(
				txSigner,
				db,
				uc.log,
				keys,
				granter,
			)

			if voteHandler == nil {
				uc.log.Error().
					Str("chain_id", chainID).
					Msg("NewVoteHandler returned nil")
				return
			}

			voteHandlers[chainID] = voteHandler
			processedChains++

			uc.log.Info().
				Str("chain_id", chainID).
				Msg("✅ Created vote handler for chain")
		}()
	}

	uc.log.Info().
		Int("processed_chains", processedChains).
		Int("skipped_chains", skippedChains).
		Int("total_vote_handlers", len(voteHandlers)).
		Msg("Vote handler creation summary")

	uc.voteHandlers = voteHandlers

	// Update chain registry with the new vote handlers (convert to interface type)
	interfaceHandlers := make(map[string]common.VoteHandler)
	for chainID, handler := range voteHandlers {
		interfaceHandlers[chainID] = handler
	}

	uc.log.Info().
		Int("interface_handlers", len(interfaceHandlers)).
		Msg("Updating chain registry with vote handlers")

	uc.chainRegistry.SetVoteHandlers(interfaceHandlers)

	uc.log.Info().
		Int("vote_handlers", len(voteHandlers)).
		Msg("✅ Successfully created per-chain vote handlers and updated chain registry")

	// Store the keys for later use when chains are added
	uc.keys = keys

	// Process any pending chains that were added before keys were available
	if len(uc.pendingVoteHandlerChains) > 0 {
		uc.log.Info().
			Int("pending_chains", len(uc.pendingVoteHandlerChains)).
			Msg("Processing pending chains for vote handler creation")

		for chainID := range uc.pendingVoteHandlerChains {
			uc.log.Debug().
				Str("chain_id", chainID).
				Msg("Creating vote handler for pending chain")
			uc.createVoteHandlerForChain(chainID)
		}

		// Clear the pending list
		uc.pendingVoteHandlerChains = make(map[string]bool)
	}
}
