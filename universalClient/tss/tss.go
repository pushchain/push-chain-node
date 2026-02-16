package tss

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/pkg/errors"
	"github.com/rs/zerolog"

	"github.com/pushchain/push-chain-node/universalClient/chains"
	"github.com/pushchain/push-chain-node/universalClient/db"
	"github.com/pushchain/push-chain-node/universalClient/pushcore"
	"github.com/pushchain/push-chain-node/universalClient/pushsigner"
	"github.com/pushchain/push-chain-node/universalClient/tss/coordinator"
	"github.com/pushchain/push-chain-node/universalClient/tss/eventstore"
	"github.com/pushchain/push-chain-node/universalClient/tss/keyshare"
	"github.com/pushchain/push-chain-node/universalClient/tss/networking"
	libp2pnet "github.com/pushchain/push-chain-node/universalClient/tss/networking/libp2p"
	"github.com/pushchain/push-chain-node/universalClient/tss/reverthandler"
	"github.com/pushchain/push-chain-node/universalClient/tss/sessionmanager"
)

// Config holds configuration for initializing a TSS node.
type Config struct {
	ValidatorAddress string
	P2PPrivateKeyHex string
	LibP2PListen     string
	HomeDir          string
	Password         string
	Database         *db.DB
	PushCore         *pushcore.Client
	Logger           zerolog.Logger

	// Optional configuration
	PollInterval     time.Duration
	CoordinatorRange uint64
	ProtocolID        string
	DialTimeout       time.Duration
	IOTimeout         time.Duration

	// Chains manager (required for sign operations to get txBuilders)
	Chains *chains.Chains

	// Session expiry checker configuration
	SessionExpiryTime          time.Duration // How long a session can be inactive before expiring (default: 5m)
	SessionExpiryCheckInterval time.Duration // How often to check for expired sessions (default: 30s)
	SessionExpiryBlockDelay    uint64        // How many blocks to delay retry after expiry (default: 10)

	// Voting configuration
	PushSigner *pushsigner.Signer // Optional - nil if voting disabled
}

// convertPrivateKeyHexToBase64 converts a hex-encoded Ed25519 private key to base64-encoded libp2p format.
func convertPrivateKeyHexToBase64(hexKey string) (string, error) {
	hexKey = strings.TrimSpace(hexKey)
	keyBytes, err := hex.DecodeString(hexKey)
	if err != nil {
		return "", fmt.Errorf("hex decode failed: %w", err)
	}
	if len(keyBytes) != 32 {
		return "", fmt.Errorf("wrong key length: got %d bytes, expected 32", len(keyBytes))
	}

	privKey := ed25519.NewKeyFromSeed(keyBytes)
	pubKey := privKey.Public().(ed25519.PublicKey)

	libp2pKeyBytes := make([]byte, 64)
	copy(libp2pKeyBytes[:32], privKey[:32])
	copy(libp2pKeyBytes[32:], pubKey)

	libp2pPrivKey, err := crypto.UnmarshalEd25519PrivateKey(libp2pKeyBytes)
	if err != nil {
		return "", fmt.Errorf("failed to unmarshal Ed25519 key: %w", err)
	}

	marshaled, err := crypto.MarshalPrivateKey(libp2pPrivKey)
	if err != nil {
		return "", fmt.Errorf("marshal failed: %w", err)
	}

	return base64.StdEncoding.EncodeToString(marshaled), nil
}

// Node represents a TSS node that can participate in TSS operations.
type Node struct {
	validatorAddress string
	network          networking.Network
	keyshareManager  *keyshare.Manager
	database         *db.DB
	pushCore         *pushcore.Client
	chains           *chains.Chains
	logger           zerolog.Logger
	eventStore       *eventstore.Store
	coordinator      *coordinator.Coordinator
	sessionManager   *sessionmanager.SessionManager
	revertHandler    *reverthandler.Handler

	// Network configuration (used during Start)
	networkCfg libp2pnet.Config

	// Coordinator configuration
	coordinatorRange        uint64
	coordinatorPollInterval time.Duration

	// Session expiry checker configuration
	sessionExpiryTime          time.Duration
	sessionExpiryCheckInterval time.Duration
	sessionExpiryBlockDelay    uint64

	// Voting configuration
	pushSigner *pushsigner.Signer // Optional - nil if voting disabled

	// Internal state
	mu           sync.RWMutex
	running      bool
	stopCh       chan struct{}
	processingWg sync.WaitGroup

	// Registered peers tracking
	registeredPeersMu sync.RWMutex
	registeredPeers   map[string]bool // peerID -> registered
}

// NewNode initializes a new TSS node.
func NewNode(ctx context.Context, cfg Config) (*Node, error) {
	if cfg.ValidatorAddress == "" {
		return nil, fmt.Errorf("validator address is required")
	}
	if cfg.P2PPrivateKeyHex == "" {
		return nil, fmt.Errorf("private key is required")
	}
	if cfg.PushCore == nil {
		return nil, fmt.Errorf("pushCore is required")
	}
	if cfg.HomeDir == "" {
		return nil, fmt.Errorf("home directory is required")
	}
	if cfg.Database == nil {
		return nil, fmt.Errorf("database is required")
	}

	logger := cfg.Logger.With().
		Str("component", "tss_node").
		Str("validator", cfg.ValidatorAddress).
		Logger()

	// Use provided home directory
	home := cfg.HomeDir

	// Initialize keyshare manager
	mgr, err := keyshare.NewManager(home, cfg.Password)
	if err != nil {
		return nil, fmt.Errorf("failed to create keyshare manager: %w", err)
	}

	// Convert private key
	privateKeyBase64, err := convertPrivateKeyHexToBase64(cfg.P2PPrivateKeyHex)
	if err != nil {
		return nil, fmt.Errorf("invalid private key: %w", err)
	}

	// Setup networking configuration (will be used in Start)
	networkCfg := libp2pnet.Config{
		ListenAddrs:      []string{cfg.LibP2PListen},
		PrivateKeyBase64: privateKeyBase64,
	}
	if cfg.ProtocolID != "" {
		networkCfg.ProtocolID = cfg.ProtocolID
	}
	if cfg.DialTimeout > 0 {
		networkCfg.DialTimeout = cfg.DialTimeout
	}
	if cfg.IOTimeout > 0 {
		networkCfg.IOTimeout = cfg.IOTimeout
	}

	// Use provided database
	database := cfg.Database

	// Set defaults
	pollInterval := cfg.PollInterval
	if pollInterval == 0 {
		pollInterval = 10 * time.Second
	}
	coordinatorRange := cfg.CoordinatorRange
	if coordinatorRange == 0 {
		coordinatorRange = 1000
	}

	sessionExpiryTime := cfg.SessionExpiryTime
	if sessionExpiryTime == 0 {
		sessionExpiryTime = 2 * time.Minute // Default: 2 minutes
	}

	sessionExpiryCheckInterval := cfg.SessionExpiryCheckInterval
	if sessionExpiryCheckInterval == 0 {
		sessionExpiryCheckInterval = 30 * time.Second // Default: check every 30 seconds
	}

	sessionExpiryBlockDelay := cfg.SessionExpiryBlockDelay
	if sessionExpiryBlockDelay == 0 {
		sessionExpiryBlockDelay = 60 // Default: retry after 60 blocks ( Approx 1 Minute for PC)
	}

	// Create event store for database access
	evtStore := eventstore.NewStore(database.Client(), logger)

	// Create revert handler (no runtime dependencies — safe to create here)
	rvHandler := reverthandler.NewHandler(reverthandler.Config{
		EventStore:    evtStore,
		PushCore:      cfg.PushCore,
		Chains:        cfg.Chains,
		PushSigner:    cfg.PushSigner,
		CheckInterval: sessionExpiryCheckInterval,
		Logger:        logger,
	})

	// Coordinator and session manager are created in Start() because they
	// depend on the network send function which requires libp2p to be running.
	node := &Node{
		validatorAddress:           cfg.ValidatorAddress,
		keyshareManager:            mgr,
		database:                   database,
		pushCore:                   cfg.PushCore,
		chains:                     cfg.Chains,
		logger:                     logger,
		eventStore:                 evtStore,
		revertHandler:              rvHandler,
		networkCfg:                 networkCfg,
		coordinatorRange:           coordinatorRange,
		coordinatorPollInterval:    pollInterval,
		sessionExpiryTime:          sessionExpiryTime,
		sessionExpiryCheckInterval: sessionExpiryCheckInterval,
		sessionExpiryBlockDelay:    sessionExpiryBlockDelay,
		pushSigner:                 cfg.PushSigner,
		stopCh:                     make(chan struct{}),
		registeredPeers:            make(map[string]bool),
	}

	return node, nil
}

// Start starts the TSS node and begins processing events.
func (n *Node) Start(ctx context.Context) error {
	n.mu.Lock()
	if n.running {
		n.mu.Unlock()
		return errors.New("node is already running")
	}
	n.running = true
	n.mu.Unlock()

	n.logger.Info().Msg("starting TSS node")

	// Start libp2p network
	net, err := libp2pnet.New(ctx, n.networkCfg, n.logger)
	if err != nil {
		return fmt.Errorf("failed to start libp2p network: %w", err)
	}
	n.network = net

	// Register global message handler
	if err := net.RegisterHandler(n.onReceive); err != nil {
		net.Close()
		return fmt.Errorf("failed to register message handler: %w", err)
	}

	n.logger.Info().
		Str("peer_id", net.ID()).
		Strs("addrs", net.ListenAddrs()).
		Msg("libp2p network started")

	// Reset all IN_PROGRESS events to PENDING on startup
	// This handles cases where the node crashed while events were in progress,
	// causing sessions to be lost from memory but events remaining in IN_PROGRESS state
	resetCount, err := n.eventStore.ResetInProgressEventsToConfirmed()
	if err != nil {
		n.logger.Warn().Err(err).Msg("failed to reset IN_PROGRESS events to PENDING, continuing anyway")
	} else if resetCount > 0 {
		n.logger.Info().
			Int64("reset_count", resetCount).
			Msg("reset IN_PROGRESS events to PENDING on node startup")
	}

	// Create coordinator with send function using node's Send method
	if n.coordinator == nil {
		coord := coordinator.NewCoordinator(
			n.eventStore,
			n.pushCore,
			n.keyshareManager,
			n.chains, // Chains manager for getting txBuilders
			n.validatorAddress,
			n.coordinatorRange,
			n.coordinatorPollInterval,
			func(ctx context.Context, peerID string, data []byte) error {
				return n.Send(ctx, peerID, data)
			},
			n.logger,
		)
		n.coordinator = coord
	}

	// Create session manager (needs coordinator reference)
	if n.sessionManager == nil {
		sessionMgr := sessionmanager.NewSessionManager(
			n.eventStore,
			n.coordinator,
			n.keyshareManager,
			n.pushCore, // For gas price verification
			n.chains,   // Chains manager for getting txBuilders
			func(ctx context.Context, peerID string, data []byte) error {
				return n.Send(ctx, peerID, data)
			},
			n.validatorAddress, // partyID for DKLS sessions
			n.sessionExpiryTime,
			n.sessionExpiryCheckInterval,
			n.sessionExpiryBlockDelay,
			n.logger,
			n.pushSigner,
		)
		n.sessionManager = sessionMgr
	}

	// Start coordinator
	n.coordinator.Start(ctx)

	// Start session manager (includes session expiry checker)
	n.sessionManager.Start(ctx)

	// Start revert handler (processes FAILED and block-expired events)
	n.revertHandler.Start(ctx)

	n.logger.Info().
		Str("peer_id", net.ID()).
		Strs("addrs", net.ListenAddrs()).
		Msg("TSS node started and ready")

	return nil
}

// Stop stops the TSS node.
func (n *Node) Stop() error {
	n.mu.Lock()
	if !n.running {
		n.mu.Unlock()
		return nil
	}
	n.running = false
	close(n.stopCh)
	n.mu.Unlock()

	n.logger.Info().Msg("stopping TSS node")

	// Stop coordinator
	n.coordinator.Stop()

	// Wait for processing to finish
	n.processingWg.Wait()

	// Close resources
	var errs []error
	if n.network != nil {
		if err := n.network.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close network: %w", err))
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

// Send sends a message to a peer.
// If peerID is the node's own peerID, it calls onReceive directly instead of sending over network.
// If the peer is not registered, it will automatically register it from validators before sending.
func (n *Node) Send(ctx context.Context, peerID string, data []byte) error {
	if n.network == nil {
		return errors.New("network not initialized")
	}

	// If sending to self, call onReceive directly
	if peerID == n.network.ID() {
		n.onReceive(peerID, data)
		return nil
	}

	// Check if peer is registered
	n.registeredPeersMu.RLock()
	isRegistered := n.registeredPeers[peerID]
	n.registeredPeersMu.RUnlock()

	// If not registered, register it using coordinator
	if !isRegistered {
		if n.coordinator == nil {
			return errors.New("coordinator not initialized")
		}

		multiaddrs, err := n.coordinator.GetMultiAddrsFromPeerID(ctx, peerID)
		if err != nil {
			return errors.Wrapf(err, "failed to get multiaddrs for peer %s", peerID)
		}

		if len(multiaddrs) == 0 {
			return errors.Errorf("peer %s has no addresses", peerID)
		}

		if err := n.network.EnsurePeer(peerID, multiaddrs); err != nil {
			return errors.Wrapf(err, "failed to register peer %s", peerID)
		}

		// Mark as registered
		n.registeredPeersMu.Lock()
		n.registeredPeers[peerID] = true
		n.registeredPeersMu.Unlock()

		n.logger.Debug().
			Str("peer_id", peerID).
			Strs("addrs", multiaddrs).
			Msg("registered peer on-demand")
	}

	// Send message
	return n.network.Send(ctx, peerID, data)
}

// onReceive handles incoming messages from p2p network.
// It passes raw data directly to sessionManager.
func (n *Node) onReceive(peerID string, data []byte) {
	ctx := context.Background()

	// Unmarshal to check message type
	var msg coordinator.Message
	if err := json.Unmarshal(data, &msg); err == nil {
		// If it's an ACK message, route it to coordinator only (not session manager)
		if msg.Type == "ack" {
			if err := n.HandleACKMessage(ctx, peerID, &msg); err != nil {
				n.logger.Warn().
					Err(err).
					Str("peer_id", peerID).
					Str("event_id", msg.EventID).
					Msg("failed to handle ACK message")
			}
			return // ACK messages are handled by coordinator only
		}
	}

	// Pass non-ACK messages to session manager
	if err := n.sessionManager.HandleIncomingMessage(ctx, peerID, data); err != nil {
		n.logger.Warn().
			Err(err).
			Str("peer_id", peerID).
			Int("data_len", len(data)).
			Msg("failed to handle incoming message")
	}
}

// HandleACKMessage handles ACK messages and forwards them to coordinator.
// This allows coordinator to track ACKs even when it's not a participant.
func (n *Node) HandleACKMessage(ctx context.Context, senderPeerID string, msg *coordinator.Message) error {
	if n.coordinator == nil {
		return errors.New("coordinator not initialized")
	}

	// Forward ACK to coordinator for tracking
	return n.coordinator.HandleACK(ctx, senderPeerID, msg.EventID)
}

// PeerID returns the libp2p peer ID (helper function).
func (n *Node) PeerID() string {
	if n.network == nil {
		return ""
	}
	return n.network.ID()
}

// ListenAddrs returns the libp2p listen addresses (helper function).
func (n *Node) ListenAddrs() []string {
	if n.network == nil {
		return nil
	}
	return n.network.ListenAddrs()
}
