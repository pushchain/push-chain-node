package core

import (
	"context"
	"fmt"
	"strings"
	"sync"
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

	uauthz "github.com/pushchain/push-chain-node/universalClient/authz"
	"github.com/pushchain/push-chain-node/universalClient/config"
	"github.com/pushchain/push-chain-node/universalClient/constant"
	"github.com/pushchain/push-chain-node/universalClient/keys"
	uetypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
	"github.com/rs/zerolog"
)

// KeyMonitor monitors keyring for new keys and validates AuthZ permissions
type KeyMonitor struct {
	ctx           context.Context
	log           zerolog.Logger
	config        *config.Config
	grpcURL       string
	checkInterval time.Duration
	
	// Callbacks for when keys change
	onValidKeyFound func(keys.UniversalValidatorKeys)
	onNoValidKey    func()
	
	// State
	mu           sync.RWMutex
	currentTxSigner TxSignerInterface
	lastValidKey    string
	lastGranter     string // Track last granter to detect changes
	stopCh          chan struct{}
}

// NewKeyMonitor creates a new key monitor
func NewKeyMonitor(
	ctx context.Context,
	log zerolog.Logger,
	config *config.Config,
	grpcURL string,
	checkInterval time.Duration,
) *KeyMonitor {
	return &KeyMonitor{
		ctx:           ctx,
		log:           log.With().Str("component", "key_monitor").Logger(),
		config:        config,
		grpcURL:       grpcURL,
		checkInterval: checkInterval,
		stopCh:        make(chan struct{}),
	}
}

// SetCallbacks sets the callbacks for key state changes
func (km *KeyMonitor) SetCallbacks(
	onValidKeyFound func(keys.UniversalValidatorKeys),
	onNoValidKey func(),
) {
	km.onValidKeyFound = onValidKeyFound
	km.onNoValidKey = onNoValidKey
}

// Start begins monitoring for keys
func (km *KeyMonitor) Start() error {
	km.log.Info().
		Dur("check_interval", km.checkInterval).
		Msg("Starting key monitor")
	
	// Start monitoring loop first (it will do initial check)
	go km.monitorLoop()
	
	return nil
}

// Stop stops the key monitor
func (km *KeyMonitor) Stop() {
	km.log.Info().Msg("Stopping key monitor")
	close(km.stopCh)
}

// monitorLoop runs the monitoring loop
func (km *KeyMonitor) monitorLoop() {
	// Do initial check immediately
	km.log.Info().Msg("Performing initial key check")
	if err := km.checkKeys(); err != nil {
		km.log.Error().
			Err(err).
			Msg("Initial key check failed")
	}
	
	ticker := time.NewTicker(km.checkInterval)
	defer ticker.Stop()
	
	for {
		select {
		case <-km.ctx.Done():
			return
		case <-km.stopCh:
			return
		case <-ticker.C:
			km.log.Debug().Msg("Performing periodic key check")
			if err := km.checkKeys(); err != nil {
				km.log.Error().
					Err(err).
					Msg("Key check failed")
			}
		}
	}
}

// checkKeys checks for valid keys with MsgVoteInbound permission
func (km *KeyMonitor) checkKeys() error {
	km.log.Debug().Msg("Checking keyring for valid keys with MsgVoteInbound permission")
	
	// Setup keyring
	keyringPath := constant.DefaultNodeHome
	km.log.Debug().
		Str("keyring_path", keyringPath).
		Str("keyring_backend", string(km.config.KeyringBackend)).
		Msg("Creating keyring")
		
	kr, err := keys.CreateKeyringFromConfig(keyringPath, nil, km.config.KeyringBackend)
	if err != nil {
		return fmt.Errorf("failed to create keyring: %w", err)
	}
	
	// List all keys
	keyInfos, err := kr.List()
	if err != nil {
		return fmt.Errorf("failed to list keys: %w", err)
	}
	
	if len(keyInfos) == 0 {
		km.log.Warn().Msg("No keys found in keyring")
		km.handleNoValidKey()
		return nil
	}
	
	km.log.Info().
		Int("key_count", len(keyInfos)).
		Msg("Found keys in keyring")
	
	// Create gRPC connection for AuthZ queries
	// Ensure proper port handling
	grpcEndpoint := km.grpcURL
	
	// Check if the URL already contains a port
	// If it ends with a port number, use as-is; otherwise add :9090
	if !strings.Contains(grpcEndpoint, ":") {
		// No port at all, add default
		grpcEndpoint = grpcEndpoint + ":9090"
	} else {
		// Has at least one colon - check if it's a port or part of hostname
		lastColon := strings.LastIndex(grpcEndpoint, ":")
		afterColon := grpcEndpoint[lastColon+1:]
		
		// Check if what's after the last colon is a number (port)
		if _, err := fmt.Sscanf(afterColon, "%d", new(int)); err != nil {
			// Not a number, so no port specified yet
			grpcEndpoint = grpcEndpoint + ":9090"
		}
		// Otherwise it already has a port, use as-is
	}
	
	km.log.Debug().
		Str("grpc_endpoint", grpcEndpoint).
		Msg("Connecting to gRPC endpoint for AuthZ queries")
	
	conn, err := grpc.NewClient(grpcEndpoint, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("failed to connect to gRPC endpoint: %w", err)
	}
	defer conn.Close()
	
	authzClient := authz.NewQueryClient(conn)
	
	// Check each key for MsgVoteInbound permission
	for _, keyInfo := range keyInfos {
		keyAddr, err := keyInfo.GetAddress()
		if err != nil {
			km.log.Warn().
				Str("key_name", keyInfo.Name).
				Err(err).
				Msg("Failed to get key address")
			continue
		}
		
		km.log.Debug().
			Str("key_name", keyInfo.Name).
			Str("key_address", keyAddr.String()).
			Msg("Checking key for MsgVoteInbound permission")
		
		// Check if this key has MsgVoteInbound permission
		// The key needs to be a grantee with permission from some granter
		hasPermission, granter := km.checkMsgVoteInboundPermission(authzClient, keyAddr.String())
		
		if hasPermission {
			// Check if this is a state change
			km.mu.Lock()
			isNewKey := km.lastValidKey != keyInfo.Name
			isNewGranter := km.lastGranter != granter
			stateChanged := isNewKey || isNewGranter
			
			if stateChanged {
				// Log at Info level for state changes
				km.log.Info().
					Str("key_name", keyInfo.Name).
					Str("key_address", keyAddr.String()).
					Str("granter", granter).
					Str("previous_key", km.lastValidKey).
					Str("previous_granter", km.lastGranter).
					Msg("✅ Key state changed - found key with MsgVoteInbound permission")
				
				if err := km.setupVoteHandler(kr, keyInfo, granter); err != nil {
					km.log.Error().
						Str("key_name", keyInfo.Name).
						Err(err).
						Msg("Failed to setup vote handler")
					km.mu.Unlock()
					continue
				}
				km.lastValidKey = keyInfo.Name
				km.lastGranter = granter
			} else {
				// Log at Debug level for unchanged state
				km.log.Debug().
					Str("key_name", keyInfo.Name).
					Str("granter", granter).
					Msg("Key with MsgVoteInbound permission unchanged")
			}
			km.mu.Unlock()
			
			return nil
		}
		
		km.log.Debug().
			Str("key_name", keyInfo.Name).
			Str("key_address", keyAddr.String()).
			Msg("Key does not have MsgVoteInbound permission")
	}
	
	// No valid keys found
	km.handleNoValidKey()
	return nil
}

// checkMsgVoteInboundPermission checks if a key has MsgVoteInbound permission as grantee
func (km *KeyMonitor) checkMsgVoteInboundPermission(authzClient authz.QueryClient, granteeAddr string) (bool, string) {
	// Create context with timeout to prevent hanging
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	
	km.log.Debug().
		Str("grantee", granteeAddr).
		Msg("Querying grantee grants")
	
	// Query all grants where this address is the grantee
	grantResp, err := authzClient.GranteeGrants(ctx, &authz.QueryGranteeGrantsRequest{
		Grantee: granteeAddr,
	})
	
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			km.log.Error().
				Str("grantee", granteeAddr).
				Msg("Timeout querying grantee grants - gRPC call took too long")
		} else {
			km.log.Warn().
				Str("grantee", granteeAddr).
				Err(err).
				Msg("Failed to query grantee grants")
		}
		return false, ""
	}
	
	km.log.Debug().
		Str("grantee", granteeAddr).
		Int("grant_count", len(grantResp.Grants)).
		Msg("Retrieved grants for grantee")
	
	// Look for MsgVoteInbound permission
	for i, grant := range grantResp.Grants {
		if grant.Authorization == nil {
			km.log.Debug().
				Int("grant_index", i).
				Msg("Skipping grant with nil authorization")
			continue
		}
		
		km.log.Debug().
			Int("grant_index", i).
			Str("granter", grant.Granter).
			Str("grantee", grant.Grantee).
			Str("auth_type", grant.Authorization.TypeUrl).
			Msg("Checking grant")
		
		// Check if this is a MsgVoteInbound authorization
		authzAny := grant.Authorization
		if authzAny.TypeUrl == "/cosmos.authz.v1beta1.GenericAuthorization" {
			// Create a new GenericAuthorization and unmarshal directly
			var genericAuth authz.GenericAuthorization
			
			// Create codec for unmarshaling
			interfaceRegistry := keys.CreateInterfaceRegistryWithEVMSupport()
			authz.RegisterInterfaces(interfaceRegistry)
			uetypes.RegisterInterfaces(interfaceRegistry)
			cdc := codec.NewProtoCodec(interfaceRegistry)
			
			// Unmarshal the value directly
			if err := cdc.Unmarshal(authzAny.Value, &genericAuth); err != nil {
				km.log.Warn().
					Int("grant_index", i).
					Err(err).
					Str("raw_value", string(authzAny.Value)).
					Msg("Failed to unmarshal generic authorization")
				continue
			}
			
			km.log.Debug().
				Int("grant_index", i).
				Str("msg_type", genericAuth.Msg).
				Msg("Generic authorization message type")
			
			if genericAuth.Msg == "/uexecutor.v1.MsgVoteInbound" {
				// Check expiration
				if grant.Expiration != nil && grant.Expiration.Before(time.Now()) {
					km.log.Warn().
						Str("granter", grant.Granter).
						Str("grantee", granteeAddr).
						Time("expiration", *grant.Expiration).
						Msg("MsgVoteInbound grant expired")
					continue
				}
				
				km.log.Debug().
					Str("granter", grant.Granter).
					Str("grantee", granteeAddr).
					Msg("Found valid MsgVoteInbound grant")
				
				return true, grant.Granter
			}
		}
	}
	
	km.log.Warn().
		Str("grantee", granteeAddr).
		Msg("No MsgVoteInbound permission found after checking all grants")
	
	return false, ""
}


// setupVoteHandler creates a new vote handler for the valid key
func (km *KeyMonitor) setupVoteHandler(kr keyring.Keyring, keyInfo *keyring.Record, granter string) error {
	keyAddr, err := keyInfo.GetAddress()
	if err != nil {
		return fmt.Errorf("failed to get key address: %w", err)
	}
	
	km.log.Info().
		Str("key_name", keyInfo.Name).
		Str("key_address", keyAddr.String()).
		Str("granter", granter).
		Msg("Setting up vote handler with valid key")
	
	// Create Keys instance
	universalKeys := keys.NewKeysWithKeybase(
		kr,
		keyAddr,
		keyInfo.Name,
		"", // Password will be prompted if needed
	)
	
	// Create client.Context for AuthZ TxSigner
	// Ensure proper port handling (same logic as in checkKeys)
	grpcEndpoint := km.grpcURL
	if !strings.Contains(grpcEndpoint, ":") {
		grpcEndpoint = grpcEndpoint + ":9090"
	} else {
		lastColon := strings.LastIndex(grpcEndpoint, ":")
		afterColon := grpcEndpoint[lastColon+1:]
		if _, err := fmt.Sscanf(afterColon, "%d", new(int)); err != nil {
			grpcEndpoint = grpcEndpoint + ":9090"
		}
	}
	
	conn, err := grpc.NewClient(grpcEndpoint, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("failed to connect to gRPC endpoint: %w", err)
	}
	
	// Setup codec
	interfaceRegistry := keys.CreateInterfaceRegistryWithEVMSupport()
	authz.RegisterInterfaces(interfaceRegistry)
	authtypes.RegisterInterfaces(interfaceRegistry)
	banktypes.RegisterInterfaces(interfaceRegistry)
	stakingtypes.RegisterInterfaces(interfaceRegistry)
	govtypes.RegisterInterfaces(interfaceRegistry)
	uetypes.RegisterInterfaces(interfaceRegistry)
	
	cdc := codec.NewProtoCodec(interfaceRegistry)
	txConfig := tx.NewTxConfig(cdc, []signing.SignMode{signing.SignMode_SIGN_MODE_DIRECT})
	
	// Create HTTP RPC client for broadcasting (standard port 26657)
	// Extract base endpoint without port
	rpcEndpoint := km.grpcURL
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
		universalKeys,
		clientCtx,
		km.log,
	)
	
	// Store the tx signer for use by the client
	km.currentTxSigner = txSigner
	
	// Notify callback - client will create per-chain vote handlers
	if km.onValidKeyFound != nil {
		km.onValidKeyFound(universalKeys)
	}
	
	km.log.Info().
		Str("key_name", keyInfo.Name).
		Str("granter", granter).
		Msg("✅ Voting handler configured successfully")
	
	return nil
}

// handleNoValidKey handles the case when no valid key is found
func (km *KeyMonitor) handleNoValidKey() {
	km.mu.Lock()
	defer km.mu.Unlock()
	
	if km.lastValidKey != "" || km.lastGranter != "" {
		km.log.Warn().
			Str("previous_key", km.lastValidKey).
			Str("previous_granter", km.lastGranter).
			Msg("Key state changed - no keys with MsgVoteInbound permission found, voting disabled")
		
		km.lastValidKey = ""
		km.lastGranter = ""
		km.currentTxSigner = nil
		
		if km.onNoValidKey != nil {
			km.onNoValidKey()
		}
	} else {
		// No change - still no valid key
		km.log.Debug().Msg("No keys with MsgVoteInbound permission - state unchanged")
	}
}

// GetCurrentTxSigner returns the current tx signer (may be nil)
func (km *KeyMonitor) GetCurrentTxSigner() TxSignerInterface {
	km.mu.RLock()
	defer km.mu.RUnlock()
	return km.currentTxSigner
}

// GetCurrentGranter returns the current granter address (may be empty)
func (km *KeyMonitor) GetCurrentGranter() string {
	km.mu.RLock()
	defer km.mu.RUnlock()
	return km.lastGranter
}