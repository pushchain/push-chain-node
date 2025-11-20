package node

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/rs/zerolog"

	"github.com/pushchain/push-chain-node/universalClient/db"
	"github.com/pushchain/push-chain-node/universalClient/tss"
	"github.com/pushchain/push-chain-node/universalClient/tss/coordinator"
	"github.com/pushchain/push-chain-node/universalClient/tss/core"
	"github.com/pushchain/push-chain-node/universalClient/tss/keyshare"
	libp2ptransport "github.com/pushchain/push-chain-node/universalClient/tss/transport/libp2p"
)

// Node represents a TSS node that can participate in TSS operations.
type Node struct {
	validatorAddress string
	coordinator      *coordinator.Coordinator
	service          *core.Service
	transport        *libp2ptransport.Transport
	keyshareManager  *keyshare.Manager
	database         *db.DB
	logger           zerolog.Logger
}

// Config holds configuration for initializing a TSS node.
type Config struct {
	// ValidatorAddress is the unique validator address for this node (used as PartyID)
	ValidatorAddress string

	// PrivateKeyHex is the Ed25519 private key in hex format (32 bytes)
	// This is used to derive the libp2p peer identity
	PrivateKeyHex string

	// LibP2PListen is the multiaddr string for libp2p to listen on
	// Example: "/ip4/127.0.0.1/tcp/39001"
	LibP2PListen string

	// HomeDir is the directory for keyshare storage
	// If empty, a temporary directory will be created
	HomeDir string

	// Password is the encryption password for keyshares
	Password string

	// Database is the database instance for storing TSS events
	// If nil, a new database will be created
	Database *db.DB

	// DataProvider provides access to Push Chain data (validators, block numbers)
	DataProvider coordinator.PushChainDataProvider

	// Logger is the logger instance
	Logger zerolog.Logger

	// CoordinatorConfig allows customizing coordinator behavior
	CoordinatorConfig *CoordinatorConfig

	// ServiceConfig allows customizing TSS service behavior
	ServiceConfig *ServiceConfig

	// TransportConfig allows customizing libp2p transport behavior
	TransportConfig *TransportConfig
}

// CoordinatorConfig allows customizing coordinator behavior.
type CoordinatorConfig struct {
	PollInterval      time.Duration
	ProcessingTimeout time.Duration
	CoordinatorRange  uint64
}

// ServiceConfig allows customizing TSS service behavior.
type ServiceConfig struct {
	SetupTimeout   time.Duration
	MessageTimeout time.Duration
}

// TransportConfig allows customizing libp2p transport behavior.
type TransportConfig struct {
	ProtocolID  string
	DialTimeout time.Duration
	IOTimeout   time.Duration
}

// NewNode initializes a new TSS node with the given configuration.
// The node is initialized but not started. Call Start() to begin processing events.
func NewNode(ctx context.Context, cfg Config) (*Node, error) {
	if cfg.ValidatorAddress == "" {
		return nil, fmt.Errorf("validator address is required")
	}
	if cfg.PrivateKeyHex == "" {
		return nil, fmt.Errorf("private key is required")
	}
	if cfg.DataProvider == nil {
		return nil, fmt.Errorf("data provider is required")
	}

	logger := cfg.Logger.With().
		Str("component", "tss_node").
		Str("validator", cfg.ValidatorAddress).
		Logger()

	// Setup home directory
	home := cfg.HomeDir
	if home == "" {
		// Use validator address (sanitized) for temp dir name
		sanitized := strings.ReplaceAll(strings.ReplaceAll(cfg.ValidatorAddress, ":", "_"), "/", "_")
		tmp, err := os.MkdirTemp("", "tss-"+sanitized+"-")
		if err != nil {
			return nil, fmt.Errorf("failed to create temp dir: %w", err)
		}
		home = tmp
		logger.Info().Str("home", home).Msg("using temporary directory")
	}

	// Initialize keyshare manager
	mgr, err := keyshare.NewManager(home, cfg.Password)
	if err != nil {
		return nil, fmt.Errorf("failed to create keyshare manager: %w", err)
	}

	// Convert private key from hex to base64
	privateKeyBase64, err := convertPrivateKeyHexToBase64(cfg.PrivateKeyHex)
	if err != nil {
		return nil, fmt.Errorf("invalid private key: %w", err)
	}

	// Setup libp2p transport config
	transportCfg := libp2ptransport.Config{
		ListenAddrs:      []string{cfg.LibP2PListen},
		PrivateKeyBase64: privateKeyBase64,
	}
	if cfg.TransportConfig != nil {
		if cfg.TransportConfig.ProtocolID != "" {
			transportCfg.ProtocolID = cfg.TransportConfig.ProtocolID
		}
		if cfg.TransportConfig.DialTimeout > 0 {
			transportCfg.DialTimeout = cfg.TransportConfig.DialTimeout
		}
		if cfg.TransportConfig.IOTimeout > 0 {
			transportCfg.IOTimeout = cfg.TransportConfig.IOTimeout
		}
	}

	// Initialize libp2p transport
	tr, err := libp2ptransport.New(ctx, transportCfg, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to start libp2p transport: %w", err)
	}

	// Get listen addresses and peer ID
	listenAddrs := tr.ListenAddrs()
	peerID := tr.ID()
	logger.Info().
		Str("peer_id", peerID).
		Strs("addrs", listenAddrs).
		Msg("libp2p transport started")

	// Setup database
	database := cfg.Database
	if database == nil {
		// Create a default database path
		sanitized := strings.ReplaceAll(strings.ReplaceAll(cfg.ValidatorAddress, ":", "_"), "/", "_")
		dbPath := fmt.Sprintf("/tmp/tss-%s.db", sanitized)
		database, err = db.OpenFileDB("/tmp", fmt.Sprintf("tss-%s.db", sanitized), true)
		if err != nil {
			tr.Close()
			return nil, fmt.Errorf("failed to open database: %w", err)
		}
		logger.Info().Str("db_path", dbPath).Msg("using node-specific database")
	}

	// Setup TSS service config
	serviceCfg := core.Config{
		PartyID: cfg.ValidatorAddress,
		Logger:  logger,
	}
	if cfg.ServiceConfig != nil {
		if cfg.ServiceConfig.SetupTimeout > 0 {
			serviceCfg.SetupTimeout = cfg.ServiceConfig.SetupTimeout
		}
		if cfg.ServiceConfig.MessageTimeout > 0 {
			serviceCfg.MessageTimeout = cfg.ServiceConfig.MessageTimeout
		}
	}

	// Create a data provider adapter to ensure proper type resolution
	dataProviderAdapter := &dataProviderAdapter{provider: cfg.DataProvider}

	// Initialize coordinator first (needed for EventStore)
	coordCfg := coordinator.Config{
		DB:           database.Client(),
		Service:      nil, // Will be set after service creation
		DataProvider: dataProviderAdapter,
		PartyID:      cfg.ValidatorAddress,
		Logger:       logger,
	}
	if cfg.CoordinatorConfig != nil {
		if cfg.CoordinatorConfig.PollInterval > 0 {
			coordCfg.PollInterval = cfg.CoordinatorConfig.PollInterval
		}
		if cfg.CoordinatorConfig.ProcessingTimeout > 0 {
			coordCfg.ProcessingTimeout = cfg.CoordinatorConfig.ProcessingTimeout
		}
		if cfg.CoordinatorConfig.CoordinatorRange > 0 {
			coordCfg.CoordinatorRange = cfg.CoordinatorConfig.CoordinatorRange
		}
	}

	coord, err := coordinator.NewCoordinator(coordCfg)
	if err != nil {
		tr.Close()
		if database != cfg.Database {
			database.Close()
		}
		return nil, fmt.Errorf("failed to create coordinator: %w", err)
	}

	// Create an EventStore adapter that wraps the coordinator
	// This ensures proper type resolution for the EventStore interface
	eventStore := &eventStoreAdapter{coordinator: coord}

	// Initialize TSS core service with EventStore from coordinator
	service, err := core.NewService(serviceCfg, core.Dependencies{
		Transport:     tr,
		KeyshareStore: mgr,
		EventStore:    eventStore,
	})
	if err != nil {
		tr.Close()
		if database != cfg.Database {
			database.Close()
		}
		return nil, fmt.Errorf("failed to create TSS service: %w", err)
	}

	// Update coordinator with the service
	coord.SetService(service)

	node := &Node{
		validatorAddress: cfg.ValidatorAddress,
		coordinator:      coord,
		service:          service,
		transport:        tr,
		keyshareManager:  mgr,
		database:         database,
		logger:           logger,
	}

	return node, nil
}

// Start starts the TSS node and begins processing events.
func (n *Node) Start(ctx context.Context) error {
	if err := n.coordinator.Start(ctx); err != nil {
		return fmt.Errorf("failed to start coordinator: %w", err)
	}

	n.logger.Info().
		Str("peer_id", n.transport.ID()).
		Strs("addrs", n.transport.ListenAddrs()).
		Msg("TSS node started and ready")

	return nil
}

// Stop stops the TSS node and cleans up resources.
func (n *Node) Stop() error {
	var errs []error

	if n.coordinator != nil {
		if err := n.coordinator.Stop(); err != nil {
			errs = append(errs, fmt.Errorf("failed to stop coordinator: %w", err))
		}
	}

	if n.transport != nil {
		if err := n.transport.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close transport: %w", err))
		}
	}

	if n.database != nil {
		if err := n.database.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close database: %w", err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors during shutdown: %v", errs)
	}

	n.logger.Info().Msg("TSS node stopped")
	return nil
}

// PeerID returns the libp2p peer ID of this node.
func (n *Node) PeerID() string {
	if n.transport == nil {
		return ""
	}
	return n.transport.ID()
}

// ListenAddrs returns the libp2p listen addresses of this node.
func (n *Node) ListenAddrs() []string {
	if n.transport == nil {
		return nil
	}
	return n.transport.ListenAddrs()
}

// convertPrivateKeyHexToBase64 converts a hex-encoded Ed25519 private key to base64-encoded libp2p format.
func convertPrivateKeyHexToBase64(hexKey string) (string, error) {
	if hexKey == "" {
		return "", fmt.Errorf("empty key")
	}

	// Trim whitespace
	hexKey = strings.TrimSpace(hexKey)

	// Convert hex to raw Ed25519 private key bytes (32 bytes)
	keyBytes, err := hex.DecodeString(hexKey)
	if err != nil {
		return "", fmt.Errorf("hex decode failed: %w", err)
	}

	if len(keyBytes) != 32 {
		return "", fmt.Errorf("wrong key length: got %d bytes, expected 32", len(keyBytes))
	}

	// Create Ed25519 private key from seed using standard library
	privKey := ed25519.NewKeyFromSeed(keyBytes)

	// Get the public key
	pubKey := privKey.Public().(ed25519.PublicKey)

	// libp2p Ed25519 private key format: [private key (32 bytes) || public key (32 bytes)]
	// This is the raw format that libp2p expects for Ed25519
	libp2pKeyBytes := make([]byte, 64)
	copy(libp2pKeyBytes[:32], privKey[:32]) // Private key (seed)
	copy(libp2pKeyBytes[32:], pubKey)       // Public key

	// Create libp2p Ed25519 private key from raw bytes
	libp2pPrivKey, err := crypto.UnmarshalEd25519PrivateKey(libp2pKeyBytes)
	if err != nil {
		return "", fmt.Errorf("failed to unmarshal Ed25519 key: %w", err)
	}

	// Marshal to protobuf format (what libp2p expects)
	marshaled, err := crypto.MarshalPrivateKey(libp2pPrivKey)
	if err != nil {
		return "", fmt.Errorf("marshal failed: %w", err)
	}

	// Convert to base64 (libp2p transport expects base64-encoded)
	return base64.StdEncoding.EncodeToString(marshaled), nil
}

// eventStoreAdapter wraps a coordinator to implement core.EventStore interface.
// This adapter is needed to ensure proper type resolution and avoid import cycle issues.
type eventStoreAdapter struct {
	coordinator *coordinator.Coordinator
}

// GetEvent implements core.EventStore interface.
func (e *eventStoreAdapter) GetEvent(eventID string) (*core.EventInfo, error) {
	return e.coordinator.GetEvent(eventID)
}

// dataProviderAdapter wraps a PushChainDataProvider to ensure proper type resolution.
// This adapter is needed to avoid type resolution issues when passing data providers
// from external packages (like cmd/tss) to the coordinator.
type dataProviderAdapter struct {
	provider coordinator.PushChainDataProvider
}

// GetLatestBlockNum implements coordinator.PushChainDataProvider.
func (d *dataProviderAdapter) GetLatestBlockNum(ctx context.Context) (uint64, error) {
	return d.provider.GetLatestBlockNum(ctx)
}

// GetUniversalValidators implements coordinator.PushChainDataProvider.
func (d *dataProviderAdapter) GetUniversalValidators(ctx context.Context) ([]*tss.UniversalValidator, error) {
	return d.provider.GetUniversalValidators(ctx)
}

// GetUniversalValidator implements coordinator.PushChainDataProvider.
func (d *dataProviderAdapter) GetUniversalValidator(ctx context.Context, validatorAddress string) (*tss.UniversalValidator, error) {
	return d.provider.GetUniversalValidator(ctx, validatorAddress)
}
