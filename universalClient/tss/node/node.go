package node

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/pkg/errors"
	"github.com/rs/zerolog"

	"github.com/pushchain/push-chain-node/universalClient/db"
	"github.com/pushchain/push-chain-node/universalClient/tss/core"
	"github.com/pushchain/push-chain-node/universalClient/tss/eventstore"
	"github.com/pushchain/push-chain-node/universalClient/tss/keyshare"
	libp2ptransport "github.com/pushchain/push-chain-node/universalClient/tss/transport/libp2p"
)

// Node represents a TSS node that can participate in TSS operations.
type Node struct {
	validatorAddress string
	service          *core.Service
	transport        *libp2ptransport.Transport
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
	activeEvents      map[string]context.CancelFunc
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

	// Setup transport
	transportCfg := libp2ptransport.Config{
		ListenAddrs:      []string{cfg.LibP2PListen},
		PrivateKeyBase64: privateKeyBase64,
	}
	if cfg.ProtocolID != "" {
		transportCfg.ProtocolID = cfg.ProtocolID
	}
	if cfg.DialTimeout > 0 {
		transportCfg.DialTimeout = cfg.DialTimeout
	}
	if cfg.IOTimeout > 0 {
		transportCfg.IOTimeout = cfg.IOTimeout
	}

	tr, err := libp2ptransport.New(ctx, transportCfg, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to start libp2p transport: %w", err)
	}

	logger.Info().
		Str("peer_id", tr.ID()).
		Strs("addrs", tr.ListenAddrs()).
		Msg("libp2p transport started")

	// Setup database
	database := cfg.Database
	if database == nil {
		sanitized := strings.ReplaceAll(strings.ReplaceAll(cfg.ValidatorAddress, ":", "_"), "/", "_")
		database, err = db.OpenFileDB("/tmp", fmt.Sprintf("tss-%s.db", sanitized), true)
		if err != nil {
			tr.Close()
			return nil, fmt.Errorf("failed to open database: %w", err)
		}
		logger.Info().Str("db_path", fmt.Sprintf("/tmp/tss-%s.db", sanitized)).Msg("using node-specific database")
	}

	// Setup service config
	serviceCfg := core.Config{
		PartyID: cfg.ValidatorAddress,
		Logger:  logger,
	}
	if cfg.SetupTimeout > 0 {
		serviceCfg.SetupTimeout = cfg.SetupTimeout
	}
	if cfg.MessageTimeout > 0 {
		serviceCfg.MessageTimeout = cfg.MessageTimeout
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

	// Create node first (needed for eventStore adapter)
	node := &Node{
		validatorAddress:  cfg.ValidatorAddress,
		service:           nil, // Will be set after service creation
		transport:         tr,
		keyshareManager:   mgr,
		database:          database,
		dataProvider:      cfg.DataProvider,
		logger:            logger,
		eventStore:        evtStore,
		pollInterval:      pollInterval,
		processingTimeout: processingTimeout,
		coordinatorRange:  coordinatorRange,
		stopCh:            make(chan struct{}),
		activeEvents:      make(map[string]context.CancelFunc),
	}

	// Create EventStore adapter for core service (for session recovery)
	eventStoreAdapter := &eventStoreAdapter{node: node}

	// Initialize TSS service
	service, err := core.NewService(serviceCfg, core.Dependencies{
		Transport:     tr,
		KeyshareStore: mgr,
		EventStore:    eventStoreAdapter,
	})
	if err != nil {
		tr.Close()
		if database != cfg.Database {
			database.Close()
		}
		return nil, fmt.Errorf("failed to create TSS service: %w", err)
	}

	// Set service on node
	node.service = service

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
		Str("peer_id", n.transport.ID()).
		Strs("addrs", n.transport.ListenAddrs()).
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

	// Cancel all active events
	n.mu.Lock()
	for eventID, cancel := range n.activeEvents {
		n.logger.Debug().Str("event_id", eventID).Msg("canceling active event")
		cancel()
	}
	n.mu.Unlock()

	// Wait for processing to finish
	n.processingWg.Wait()

	// Close resources
	var errs []error
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

// PeerID returns the libp2p peer ID.
func (n *Node) PeerID() string {
	if n.transport == nil {
		return ""
	}
	return n.transport.ID()
}

// ListenAddrs returns the libp2p listen addresses.
func (n *Node) ListenAddrs() []string {
	if n.transport == nil {
		return nil
	}
	return n.transport.ListenAddrs()
}
