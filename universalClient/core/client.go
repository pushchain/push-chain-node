package core

import (
	"context"
	"fmt"
	"strings"
	"time"

	rpchttp "github.com/cometbft/cometbft/rpc/client/http"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	"github.com/cosmos/cosmos-sdk/x/auth/tx"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types/v1beta1"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	
	"github.com/rollchains/pchain/universalClient/api"
	uauthz "github.com/rollchains/pchain/universalClient/authz"
	"github.com/rollchains/pchain/universalClient/chains"
	"github.com/rollchains/pchain/universalClient/chains/common"
	"github.com/rollchains/pchain/universalClient/config"
	"github.com/rollchains/pchain/universalClient/constant"
	"github.com/rollchains/pchain/universalClient/db"
	"github.com/rollchains/pchain/universalClient/keys"
	"github.com/rollchains/pchain/universalClient/registry"
	uetypes "github.com/rollchains/pchain/x/ue/types"
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
	keys        keys.UniversalValidatorKeys
	voteHandler *VoteHandler
	keyMonitor  *KeyMonitor // Monitors keys and permissions dynamically
	universalDB *db.DB // Database for universal validator state (voting, etc)
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

	// Create universal DB for validator state
	universalDB, err := dbManager.GetChainDB("universal-validator")
	if err != nil {
		return nil, fmt.Errorf("failed to create universal validator database: %w", err)
	}

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
		universalDB:        universalDB,
		transactionCleaner: transactionCleaner,
	}

	// Create key monitor for dynamic key detection
	if len(cfg.PushChainGRPCURLs) > 0 {
		keyCheckInterval := 30 * time.Second // Default to 30 seconds
		if cfg.KeyCheckInterval > 0 {
			keyCheckInterval = time.Duration(cfg.KeyCheckInterval) * time.Second
		}
		
		uc.keyMonitor = NewKeyMonitor(
			ctx,
			log,
			cfg,
			universalDB,
			cfg.PushChainGRPCURLs[0],
			keyCheckInterval,
		)
		
		// Set callbacks for when keys change
		uc.keyMonitor.SetCallbacks(
			func(keys keys.UniversalValidatorKeys, voteHandler *VoteHandler) {
				uc.keys = keys
				uc.voteHandler = voteHandler
				uc.chainRegistry.SetVoteHandler(voteHandler)
			},
			func() {
				uc.keys = nil
				uc.voteHandler = nil
				uc.chainRegistry.SetVoteHandler(nil)
			},
		)
	}

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
	} else {
		// Fallback to old method if key monitor is not available
		if err := uc.loadKeysAndSetupVoting(); err != nil {
			uc.log.Warn().
				Err(err).
				Msg("Failed to load hot keys - will run in non-voting mode. To enable voting, configure hot keys")
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

// GetVoteHandler returns the vote handler (may be nil if keys not configured)
func (uc *UniversalClient) GetVoteHandler() *VoteHandler {
	return uc.voteHandler
}

// loadKeysAndSetupVoting loads hot keys and sets up voting infrastructure
func (uc *UniversalClient) loadKeysAndSetupVoting() error {
	uc.log.Info().Msg("Loading hot keys and setting up voting...")

	// Setup keyring
	var kr keyring.Keyring
	var err error
	
	// Use the home directory as the keyring path (not a subdirectory)
	// The keyring backend will create the appropriate subdirectory (keyring-test or keyring-file)
	keyringPath := constant.DefaultNodeHome
	
	switch uc.config.KeyringBackend {
	case config.KeyringBackendTest:
		kr, err = keyring.New("puniversald", keyring.BackendTest, keyringPath, nil, nil)
	case config.KeyringBackendFile:
		kr, err = keyring.New("puniversald", keyring.BackendFile, keyringPath, nil, nil)
	default:
		return fmt.Errorf("unsupported keyring backend: %s", uc.config.KeyringBackend)
	}
	
	if err != nil {
		return fmt.Errorf("failed to create keyring: %w", err)
	}

	// List all keys to find operator and hot keys
	uc.log.Info().
		Str("keyring_backend", string(uc.config.KeyringBackend)).
		Str("keyring_path", keyringPath).
		Msg("Attempting to list keys from keyring")
		
	keyInfos, err := kr.List()
	if err != nil {
		return fmt.Errorf("failed to list keys: %w", err)
	}

	uc.log.Info().
		Int("key_count", len(keyInfos)).
		Msg("Successfully listed keys from keyring")

	if len(keyInfos) == 0 {
		return fmt.Errorf("no keys found in keyring")
	}

	// For now, we'll use the first key as both operator and hot key
	// In production, these should be configured separately
	var operatorKey *keyring.Record
	var hotkeyName string
	
	for _, keyInfo := range keyInfos {
		uc.log.Info().
			Str("key_name", keyInfo.Name).
			Str("key_type", keyInfo.GetType().String()).
			Msg("Found key in keyring")
		
		// Use the first key we find
		if operatorKey == nil {
			operatorKey = keyInfo
			hotkeyName = keyInfo.Name
		}
	}

	if operatorKey == nil {
		return fmt.Errorf("no suitable keys found in keyring")
	}

	// Get operator address
	operatorAddr, err := operatorKey.GetAddress()
	if err != nil {
		return fmt.Errorf("failed to get operator address: %w", err)
	}

	uc.log.Info().
		Str("operator_address", operatorAddr.String()).
		Str("hotkey_name", hotkeyName).
		Msg("Keys loaded successfully")

	// Create Keys instance
	uc.keys = keys.NewKeysWithKeybase(
		kr,
		operatorAddr,
		hotkeyName,
		"", // Password will be prompted if needed for file backend
	)

	// Create client.Context for AuthZ TxSigner
	// Use the first gRPC URL from config
	if len(uc.config.PushChainGRPCURLs) == 0 {
		return fmt.Errorf("no PushChain gRPC URLs configured")
	}
	grpcURL := uc.config.PushChainGRPCURLs[0] + ":9090" // Add standard gRPC port
	
	// Create gRPC connection
	conn, err := grpc.NewClient(grpcURL, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("failed to connect to gRPC endpoint: %w", err)
	}
	
	// Create HTTP RPC client for broadcasting (standard port 26657)
	// Extract base endpoint without port
	rpcEndpoint := uc.config.PushChainGRPCURLs[0]
	colonIndex := strings.LastIndex(rpcEndpoint, ":")
	if colonIndex > 0 {
		// Check if it's a port number after the colon
		afterColon := rpcEndpoint[colonIndex+1:]
		if _, err := fmt.Sscanf(afterColon, "%d", new(int)); err == nil {
			// It's a port, remove it
			rpcEndpoint = rpcEndpoint[:colonIndex]
		}
	}
	
	rpcURL := "http://" + rpcEndpoint + ":26657"
	httpClient, err := rpchttp.New(rpcURL, "/websocket")
	if err != nil {
		return fmt.Errorf("failed to create RPC client: %w", err)
	}
	
	// Setup codec with all required interfaces
	interfaceRegistry := keys.CreateInterfaceRegistryWithEVMSupport()
	authz.RegisterInterfaces(interfaceRegistry)
	authtypes.RegisterInterfaces(interfaceRegistry)
	banktypes.RegisterInterfaces(interfaceRegistry)
	stakingtypes.RegisterInterfaces(interfaceRegistry)
	govtypes.RegisterInterfaces(interfaceRegistry)
	uetypes.RegisterInterfaces(interfaceRegistry)
	
	cdc := codec.NewProtoCodec(interfaceRegistry)
	txConfig := tx.NewTxConfig(cdc, []signing.SignMode{signing.SignMode_SIGN_MODE_DIRECT})
	
	// Create client context
	clientCtx := client.Context{}.
		WithCodec(cdc).
		WithInterfaceRegistry(interfaceRegistry).
		WithChainID("localchain_9000-1"). // TODO: Make this configurable
		WithKeyring(kr).
		WithGRPCClient(conn).
		WithTxConfig(txConfig).
		WithBroadcastMode("sync").
		WithClient(httpClient)

	// Create AuthZ TxSigner
	txSigner := uauthz.NewTxSigner(
		uc.keys,
		clientCtx,
		uc.log,
	)

	// Create VoteHandler
	uc.voteHandler = NewVoteHandler(
		txSigner,
		uc.universalDB, // Use universal DB for vote tracking
		uc.log,
		uc.keys,
		operatorAddr.String(), // Granter is the operator
	)

	// Set vote handler on chain registry
	uc.chainRegistry.SetVoteHandler(uc.voteHandler)

	uc.log.Info().Msg("Voting infrastructure setup complete")
	return nil
}
