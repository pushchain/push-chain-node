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

	// Unified signer components
	signerHandler *SignerHandler // Single signer for all chains

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

	// Use configured refresh interval or default to 60 seconds
	refreshInterval := time.Duration(cfg.ConfigRefreshIntervalSeconds) * time.Second
	if refreshInterval <= 0 {
		refreshInterval = 60 * time.Second
	}

	log.Info().
		Dur("refresh_interval", refreshInterval).
		Msg("Setting cache refresh interval")

	// Create cache jobs with configured interval; 8s per-sync timeout
	chainCacheJob := cron.NewChainCacheJob(cache, pushCore, refreshInterval, 8*time.Second, log)

	chainRegistryJob := cron.NewChainRegistryJob(cache, chainRegistry, refreshInterval, 8*time.Second, log)

	// Create the client
	uc := &UniversalClient{
		ctx:       ctx,
		log:       log,
		dbManager: dbManager,

		// configUpdater:            configUpdater,
		chainRegistry:      chainRegistry,
		config:             cfg,
		transactionCleaner: transactionCleaner,
		gasPriceFetcher:    gasPriceFetcher,

		pushCore:         pushCore,
		cache:            cache,
		chainCacheJob:    chainCacheJob,
		chainRegistryJob: chainRegistryJob,
	}

	// Register as observer for chain addition events
	chainRegistry.SetObserver(uc)

	// Perform mandatory startup validation
	log.Info().Msg("🔐 Validating hotkey and AuthZ permissions...")

	startupValidator := NewStartupValidator(
		ctx,
		log,
		cfg,
		cfg.PushChainGRPCURLs[0],
	)

	validationResult, err := startupValidator.ValidateStartupRequirements()
	if err != nil {
		log.Error().
			Err(err).
			Msg("❌ Startup validation failed. Universal Validator requires a valid hotkey with AuthZ permissions.")
		return nil, fmt.Errorf("startup validation failed: %w", err)
	}

	// Create unified signer handler with simplified validation result
	signerHandler, err := NewSignerHandler(ctx, log, validationResult, cfg.PushChainGRPCURLs[0])
	if err != nil {
		return nil, fmt.Errorf("failed to create signer handler: %w", err)
	}

	uc.signerHandler = signerHandler

	// Set vote handlers in chain registry
	uc.updateVoteHandlersForAllChains()

	// Create query server
	log.Info().Int("port", cfg.QueryServerPort).Msg("Creating query server")
	uc.queryServer = api.NewServer(uc, log, cfg.QueryServerPort)

	return uc, nil
}

func (uc *UniversalClient) Start() error {
	uc.log.Info().Msg("🚀 Starting universal client...")

	// Log signer status (always present after successful startup validation)
	uc.log.Info().
		Str("granter", uc.signerHandler.GetGranter()).
		Msg("✅ Voting enabled with valid hotkey")

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

	// No key monitor to stop anymore - simplified architecture

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
		Msg("New chain added, updating vote handlers")

	// Simply refresh all vote handlers when a new chain is added
	uc.updateVoteHandlersForAllChains()
}

// updateVoteHandlersForAllChains updates vote handlers for all chains to use the unified signer
func (uc *UniversalClient) updateVoteHandlersForAllChains() {
	uc.log.Info().Msg("Updating vote handlers for all chains")

	if uc.signerHandler == nil {
		uc.log.Warn().Msg("No signer handler available - vote handlers will be null")
		uc.chainRegistry.SetVoteHandlers(nil)
		return
	}

	chainDatabases := uc.dbManager.GetAllDatabases()

	// If no databases exist yet, attempt to pre-create them from the cache
	if len(chainDatabases) == 0 && uc.cache != nil {
		cached := uc.cache.GetAllChains()
		created := 0
		for _, cd := range cached {
			if cd == nil || cd.Config == nil || cd.Config.Chain == "" {
				continue
			}
			// Respect chain enabled flags
			if cd.Config.Enabled == nil || (!cd.Config.Enabled.IsInboundEnabled && !cd.Config.Enabled.IsOutboundEnabled) {
				continue
			}
			if cd.Config.Chain == "universal-validator" {
				continue
			}
			if _, exists := chainDatabases[cd.Config.Chain]; exists {
				continue
			}
			if db, err := uc.dbManager.GetChainDB(cd.Config.Chain); err == nil && db != nil {
				chainDatabases[cd.Config.Chain] = db
				created++
			} else if err != nil {
				uc.log.Warn().
					Str("chain_id", cd.Config.Chain).
					Err(err).
					Msg("Failed to pre-create database from cache")
			}
		}
		if created > 0 {
			uc.log.Info().
				Int("created", created).
				Msg("Pre-created chain databases from cache on startup")
		}
	}

	uc.log.Info().
		Int("database_count", len(chainDatabases)).
		Msg("Found chain databases")

	if len(chainDatabases) == 0 {
		uc.log.Warn().Msg("No chain databases exist yet - vote handlers will be created when chains are added")
		return
	}

	// Create vote handlers using the unified signer
	voteHandlers := make(map[string]*VoteHandler)
	processedChains := 0
	skippedChains := 0

	for chainID, db := range chainDatabases {
		// Skip the universal validator database
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

		// Create vote handler for this chain
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
				uc.signerHandler.GetTxSigner(),
				db,
				uc.log,
				uc.signerHandler.GetKeys(),
				uc.signerHandler.GetGranter(),
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
}
