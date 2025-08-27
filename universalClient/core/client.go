package core

import (
	"context"
	"fmt"
	"time"

	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/rollchains/pchain/universalClient/api"
	"github.com/rollchains/pchain/universalClient/authz"
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
	ctx context.Context
	log zerolog.Logger
	db  *db.DB

	// Registry components
	registryClient *registry.RegistryClient
	configCache    *registry.ConfigCache
	configUpdater  *ConfigUpdater
	chainRegistry  *chains.ChainRegistry
	config         *config.Config
	queryServer    *api.Server
	
	// Hot key components
	keys        keys.UniversalValidatorKeys
	authzSigner *authz.Signer
}

func NewUniversalClient(ctx context.Context, log zerolog.Logger, db *db.DB, cfg *config.Config) (*UniversalClient, error) {
	// Create registry client
	registryClient, err := registry.NewRegistryClient(cfg.PushChainGRPCURLs, log)
	if err != nil {
		return nil, fmt.Errorf("failed to create registry client: %w", err)
	}

	// Create config cache
	configCache := registry.NewConfigCache(log)

	// Create chain registry
	chainRegistry := chains.NewChainRegistry(log)

	// Create config updater
	configUpdater := NewConfigUpdater(
		registryClient,
		configCache,
		chainRegistry,
		cfg,
		log,
	)

	// Create the client
	uc := &UniversalClient{
		ctx:            ctx,
		log:            log,
		db:             db,
		registryClient: registryClient,
		configCache:    configCache,
		configUpdater:  configUpdater,
		chainRegistry:  chainRegistry,
		config:         cfg,
	}
	
	// Initialize hot key components if configured
	if config.IsHotKeyConfigured(cfg) {
		log.Info().Msg("Hot key configuration detected, initializing key management...")
		
		// Initialize keys
		keyMgr, err := keys.NewKeys(cfg.AuthzHotkey, cfg)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize keys: %w", err)
		}
		uc.keys = keyMgr
		
		// Initialize AuthZ signer
		signer := &authz.Signer{
			KeyType:        authz.UniversalValidatorHotKey,
			GranterAddress: cfg.AuthzGranter,
		}
		uc.authzSigner = signer
		
		log.Info().
			Str("granter", cfg.AuthzGranter).
			Str("hotkey", cfg.AuthzHotkey).
			Msg("Hot key management initialized")
	} else {
		log.Info().Msg("No hot key configuration found, operating in standard mode")
	}
	
	// Create query server
	log.Info().Int("port", cfg.QueryServerPort).Msg("Creating query server")
	uc.queryServer = api.NewServer(uc, log, cfg.QueryServerPort)
	
	return uc, nil
}

func (uc *UniversalClient) Start() error {
	uc.log.Info().Msg("🚀 Starting universal client...")

	// Start the config updater
	if err := uc.configUpdater.Start(uc.ctx); err != nil {
		return fmt.Errorf("failed to start config updater: %w", err)
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

	// Stop all chain clients
	uc.chainRegistry.StopAll()

	// Close registry client connection
	if err := uc.registryClient.Close(); err != nil {
		uc.log.Error().Err(err).Msg("error closing registry client")
	}

	return uc.db.Close()
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
	return uc.configUpdater.ForceUpdate(uc.ctx)
}

// GetKeys returns the hot key manager if configured
func (uc *UniversalClient) GetKeys() keys.UniversalValidatorKeys {
	return uc.keys
}

// GetAuthzSigner returns the AuthZ signer if configured
func (uc *UniversalClient) GetAuthzSigner() *authz.Signer {
	return uc.authzSigner
}

// IsHotKeyEnabled returns true if hot key management is enabled
func (uc *UniversalClient) IsHotKeyEnabled() bool {
	return uc.keys != nil
}

// CreateTxSigner creates a transaction signer for AuthZ operations
func (uc *UniversalClient) CreateTxSigner(clientCtx client.Context) (*authz.TxSigner, error) {
	if !uc.IsHotKeyEnabled() {
		return nil, fmt.Errorf("hot key management is not enabled")
	}

	return authz.NewTxSigner(uc.keys, uc.authzSigner, clientCtx, uc.log), nil
}

// SignAndBroadcastAuthZTx signs and broadcasts an AuthZ transaction using the hot key
func (uc *UniversalClient) SignAndBroadcastAuthZTx(
	ctx context.Context,
	clientCtx client.Context,
	msgs []sdk.Msg,
	memo string,
	gasLimit uint64,
	feeAmount sdk.Coins,
) (*sdk.TxResponse, error) {
	if !uc.IsHotKeyEnabled() {
		return nil, fmt.Errorf("hot key management is not enabled")
	}

	txSigner, err := uc.CreateTxSigner(clientCtx)
	if err != nil {
		return nil, fmt.Errorf("failed to create tx signer: %w", err)
	}

	return txSigner.SignAndBroadcastAuthZTx(ctx, msgs, memo, gasLimit, feeAmount)
}

// EstimateAuthZGas estimates gas for an AuthZ transaction
func (uc *UniversalClient) EstimateAuthZGas(
	ctx context.Context,
	clientCtx client.Context,
	msgs []sdk.Msg,
	memo string,
) (uint64, error) {
	if !uc.IsHotKeyEnabled() {
		return 0, fmt.Errorf("hot key management is not enabled")
	}

	txSigner, err := uc.CreateTxSigner(clientCtx)
	if err != nil {
		return 0, fmt.Errorf("failed to create tx signer: %w", err)
	}

	return txSigner.EstimateGas(ctx, msgs, memo)
}

// CreateOperationsHandler creates a high-level operations handler for Universal Validator
func (uc *UniversalClient) CreateOperationsHandler(clientCtx client.Context) (*authz.UniversalValidatorOperations, error) {
	if !uc.IsHotKeyEnabled() {
		return nil, fmt.Errorf("hot key management is not enabled")
	}

	return authz.NewUniversalValidatorOperations(uc.keys, uc.authzSigner, clientCtx, uc.log), nil
}

// ValidateHotKeyReadiness checks if the hot key is ready for operations
func (uc *UniversalClient) ValidateHotKeyReadiness(ctx context.Context, clientCtx client.Context) error {
	if !uc.IsHotKeyEnabled() {
		return fmt.Errorf("hot key management is not enabled")
	}

	ops, err := uc.CreateOperationsHandler(clientCtx)
	if err != nil {
		return fmt.Errorf("failed to create operations handler: %w", err)
	}

	return ops.ValidateOperationalReadiness(ctx)
}
