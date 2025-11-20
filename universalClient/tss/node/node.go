package node

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/pkg/errors"
	"github.com/rs/zerolog"

	"github.com/pushchain/push-chain-node/universalClient/db"
	"github.com/pushchain/push-chain-node/universalClient/tss/dkls"
	"github.com/pushchain/push-chain-node/universalClient/tss/eventstore"
	"github.com/pushchain/push-chain-node/universalClient/tss/keyshare"
	"github.com/pushchain/push-chain-node/universalClient/tss/networking"
	libp2pnet "github.com/pushchain/push-chain-node/universalClient/tss/networking/libp2p"
)

// Node represents a TSS node that can participate in TSS operations.
type Node struct {
	validatorAddress string
	network          networking.Network
	keyshareManager  *keyshare.Manager
	database         *db.DB
	dataProvider     PushChainDataProvider
	logger           zerolog.Logger
	eventStore       *eventstore.Store

	// Coordinator state
	pollInterval      time.Duration
	processingTimeout time.Duration
	coordinatorRange  uint64
	mu                sync.RWMutex
	running           bool
	stopCh            chan struct{}
	processingWg      sync.WaitGroup

	// Session management for message routing
	sessionsMu      sync.RWMutex
	sessions        map[string]dkls.Session // eventID -> session
	setupHandlersMu sync.RWMutex
	setupHandlers   map[string]func([]byte) // eventID -> setup handler
}

// NewNode initializes a new TSS node.
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

	// Convert private key
	privateKeyBase64, err := convertPrivateKeyHexToBase64(cfg.PrivateKeyHex)
	if err != nil {
		return nil, fmt.Errorf("invalid private key: %w", err)
	}

	// Setup networking
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

	net, err := libp2pnet.New(ctx, networkCfg, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to start libp2p network: %w", err)
	}

	logger.Info().
		Str("peer_id", net.ID()).
		Strs("addrs", net.ListenAddrs()).
		Msg("libp2p network started")

	// Setup database
	database := cfg.Database
	if database == nil {
		sanitized := strings.ReplaceAll(strings.ReplaceAll(cfg.ValidatorAddress, ":", "_"), "/", "_")
		database, err = db.OpenFileDB("/tmp", fmt.Sprintf("tss-%s.db", sanitized), true)
		if err != nil {
			net.Close()
			return nil, fmt.Errorf("failed to open database: %w", err)
		}
		logger.Info().Str("db_path", fmt.Sprintf("/tmp/tss-%s.db", sanitized)).Msg("using node-specific database")
	}

	// Set defaults
	pollInterval := cfg.PollInterval
	if pollInterval == 0 {
		pollInterval = 2 * time.Second
	}
	processingTimeout := cfg.ProcessingTimeout
	if processingTimeout == 0 {
		processingTimeout = 5 * time.Minute
	}
	coordinatorRange := cfg.CoordinatorRange
	if coordinatorRange == 0 {
		coordinatorRange = 100
	}

	// Create event store for database access
	evtStore := eventstore.NewStore(database.Client(), logger)

	// Create node
	node := &Node{
		validatorAddress:  cfg.ValidatorAddress,
		network:           net,
		keyshareManager:   mgr,
		database:          database,
		dataProvider:      cfg.DataProvider,
		logger:            logger,
		eventStore:        evtStore,
		pollInterval:      pollInterval,
		processingTimeout: processingTimeout,
		coordinatorRange:  coordinatorRange,
		stopCh:            make(chan struct{}),
		sessions:          make(map[string]dkls.Session),
		setupHandlers:     make(map[string]func([]byte)),
	}

	// Register global message handler once
	if err := net.RegisterHandler(node.handleIncomingMessage); err != nil {
		net.Close()
		if database != cfg.Database {
			database.Close()
		}
		return nil, fmt.Errorf("failed to register message handler: %w", err)
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
	go n.pollLoop(ctx)

	n.logger.Info().
		Str("peer_id", n.network.ID()).
		Strs("addrs", n.network.ListenAddrs()).
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

// PeerID returns the libp2p peer ID.
func (n *Node) PeerID() string {
	if n.network == nil {
		return ""
	}
	return n.network.ID()
}

// ListenAddrs returns the libp2p listen addresses.
func (n *Node) ListenAddrs() []string {
	if n.network == nil {
		return nil
	}
	return n.network.ListenAddrs()
}

// handleIncomingMessage routes incoming messages to the appropriate session.
func (n *Node) handleIncomingMessage(peerID string, data []byte) {
	// Try to parse as setup message first
	var setupMsg struct {
		Type      string `json:"type"`
		EventID   string `json:"event_id"`
		Setup     []byte `json:"setup"` // JSON will base64 decode this automatically
		Threshold int    `json:"threshold"`
	}
	if err := json.Unmarshal(data, &setupMsg); err == nil {
		if setupMsg.Type == "setup" && setupMsg.EventID != "" && len(setupMsg.Setup) > 0 {
			n.setupHandlersMu.RLock()
			handler, exists := n.setupHandlers[setupMsg.EventID]
			n.setupHandlersMu.RUnlock()
			if exists {
				handler(setupMsg.Setup)
				n.logger.Debug().
					Str("event_id", setupMsg.EventID).
					Str("peer_id", peerID).
					Int("setup_len", len(setupMsg.Setup)).
					Msg("routed setup message to handler")
				return
			}
		}
	}

	// Not a setup message, try to route to active sessions
	n.sessionsMu.RLock()
	defer n.sessionsMu.RUnlock()

	// Try all active sessions (messages will be ignored if not for that session)
	routed := false
	for eventID, session := range n.sessions {
		if err := session.InputMessage(data); err != nil {
			n.logger.Debug().
				Err(err).
				Str("event_id", eventID).
				Str("peer_id", peerID).
				Int("data_len", len(data)).
				Msg("message not for this session or failed to process")
		} else {
			n.logger.Info().
				Str("event_id", eventID).
				Str("peer_id", peerID).
				Int("data_len", len(data)).
				Msg("routed protocol message to session")
			routed = true
			break // Message was processed by this session
		}
	}

	if !routed && len(n.sessions) > 0 {
		n.logger.Warn().
			Str("peer_id", peerID).
			Int("data_len", len(data)).
			Int("active_sessions", len(n.sessions)).
			Msg("protocol message received but not routed to any session")
	}
}
