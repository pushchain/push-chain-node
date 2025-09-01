package core

import (
	"context"
	"fmt"
	"time"

	"github.com/rollchains/pchain/universalClient/api"
	"github.com/rollchains/pchain/universalClient/chains"
	"github.com/rollchains/pchain/universalClient/chains/common"
	"github.com/rollchains/pchain/universalClient/config"
	"github.com/rollchains/pchain/universalClient/db"
	"github.com/rollchains/pchain/universalClient/keys"
	"github.com/rollchains/pchain/universalClient/registry"
	uregistrytypes "github.com/rollchains/pchain/x/uregistry/types"
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
	keys keys.UniversalValidatorKeys
	// Transaction cleanup
	transactionCleaner *db.PerChainTransactionCleaner
}

func NewUniversalClient(ctx context.Context, log zerolog.Logger, dbManager *db.ChainDBManager, cfg *config.Config) (*UniversalClient, error) {
	// Validate config
	if cfg == nil {
		return nil, fmt.Errorf("config is nil")
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
		transactionCleaner: transactionCleaner,
	}

	// Create query server
	log.Info().Int("port", cfg.QueryServerPort).Msg("Creating query server")
	uc.queryServer = api.NewServer(uc, log, cfg.QueryServerPort)

	return uc, nil
}

func (uc *UniversalClient) Start() error {
	uc.log.Info().Msg("🚀 Starting universal client...")

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

	// Stop config updater
	uc.configUpdater.Stop()

	// Stop transaction cleaner
	uc.transactionCleaner.Stop()

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
