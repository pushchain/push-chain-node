package main

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/pushchain/push-chain-node/universalClient/db"
	"github.com/pushchain/push-chain-node/universalClient/store"
	"github.com/pushchain/push-chain-node/universalClient/tss"
	"github.com/pushchain/push-chain-node/universalClient/tss/coordinator"
	"github.com/pushchain/push-chain-node/universalClient/tss/core"
	"github.com/pushchain/push-chain-node/universalClient/tss/keyshare"
	libp2ptransport "github.com/pushchain/push-chain-node/universalClient/tss/transport/libp2p"
)

const (
	// nodesRegistryFile is the shared file where all nodes register themselves
	nodesRegistryFile = "/tmp/tss-nodes.json"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	command := os.Args[1]
	os.Args = os.Args[1:] // Remove command from args for flag parsing

	switch command {
	case "node":
		runNode()
	case "keygen", "keyrefresh", "sign":
		runCommand(command)
	default:
		fmt.Printf("Unknown command: %s\n", command)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println("Usage: tss <command> [flags]")
	fmt.Println("")
	fmt.Println("Commands:")
	fmt.Println("  node          Run a TSS node")
	fmt.Println("  keygen        Trigger a keygen operation")
	fmt.Println("  keyrefresh    Trigger a keyrefresh operation")
	fmt.Println("  sign          Trigger a sign operation")
	fmt.Println("")
	fmt.Println("Examples:")
	fmt.Println("  tss node -validator-address=pushvaloper1... -p2p-listen=/ip4/127.0.0.1/tcp/39001")
	fmt.Println("  tss node -validator-address=pushvaloper1... -private-key=30B0D9... -p2p-listen=/ip4/127.0.0.1/tcp/39001")
	fmt.Println("  tss keygen -node=pushvaloper1... -key-id=demo-key-1")
	fmt.Println("  tss keyrefresh -node=pushvaloper1... -key-id=demo-key-1")
	fmt.Println("  tss sign -node=pushvaloper1... -key-id=demo-key-1 -message='hello'")
}

// nodeRegistryEntry represents a single node's registration info
type nodeRegistryEntry struct {
	ValidatorAddress string    `json:"validator_address"`
	PeerID           string    `json:"peer_id"`
	Multiaddrs       []string  `json:"multiaddrs"`
	LastUpdated      time.Time `json:"last_updated"`
}

// nodeRegistry is the in-memory representation of the registry file
type nodeRegistry struct {
	Nodes []nodeRegistryEntry `json:"nodes"`
	mu    sync.RWMutex
}

var (
	registryMu sync.Mutex
)

// registerNode adds or updates a node in the shared registry file
func registerNode(node nodeRegistryEntry, logger zerolog.Logger) error {
	registryMu.Lock()
	defer registryMu.Unlock()

	// Read existing registry
	registry := &nodeRegistry{Nodes: []nodeRegistryEntry{}}
	data, err := os.ReadFile(nodesRegistryFile)
	if err == nil {
		if err := json.Unmarshal(data, registry); err != nil {
			logger.Warn().Err(err).Msg("failed to parse existing registry, creating new one")
			registry.Nodes = []nodeRegistryEntry{}
		}
	}

	// Update or add node
	found := false
	for i := range registry.Nodes {
		if registry.Nodes[i].ValidatorAddress == node.ValidatorAddress {
			registry.Nodes[i] = node
			found = true
			break
		}
	}
	if !found {
		registry.Nodes = append(registry.Nodes, node)
	}

	// Write back to file
	data, err = json.MarshalIndent(registry, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal registry: %w", err)
	}

	if err := os.WriteFile(nodesRegistryFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write registry file: %w", err)
	}

	return nil
}

// readNodeRegistry reads all nodes from the shared registry file
func readNodeRegistry(logger zerolog.Logger) ([]nodeRegistryEntry, error) {
	registryMu.Lock()
	defer registryMu.Unlock()

	data, err := os.ReadFile(nodesRegistryFile)
	if err != nil {
		if os.IsNotExist(err) {
			// File doesn't exist yet, return empty list
			return []nodeRegistryEntry{}, nil
		}
		return nil, fmt.Errorf("failed to read registry file: %w", err)
	}

	registry := &nodeRegistry{}
	if err := json.Unmarshal(data, registry); err != nil {
		return nil, fmt.Errorf("failed to parse registry file: %w", err)
	}

	return registry.Nodes, nil
}

func runNode() {
	var (
		validatorAddr = flag.String("validator-address", "", "validator address (unique per node)")
		privateKeyHex = flag.String("private-key", "", "Ed25519 private key in hex format (optional, uses hardcoded key if not provided)")
		libp2pListen  = flag.String("p2p-listen", "/ip4/127.0.0.1/tcp/0", "libp2p listen multiaddr")
		homeDir       = flag.String("home", "", "directory for keyshare storage (defaults to temp)")
		password      = flag.String("password", "demo-password", "encryption password for keyshares")
	)
	flag.Parse()

	if *validatorAddr == "" {
		fmt.Println("validator-address flag is required")
		flag.Usage()
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	logger := zerolog.New(zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: time.RFC3339}).
		With().
		Str("validator", *validatorAddr).
		Timestamp().
		Logger()

	// Setup home directory
	home := *homeDir
	if home == "" {
		// Use validator address (sanitized) for temp dir name
		sanitized := strings.ReplaceAll(strings.ReplaceAll(*validatorAddr, ":", "_"), "/", "_")
		tmp, err := os.MkdirTemp("", "tss-demo-"+sanitized+"-")
		if err != nil {
			logger.Fatal().Err(err).Msg("failed to create temp dir")
		}
		home = tmp
		defer os.RemoveAll(home)
		logger.Info().Str("home", home).Msg("using temporary directory")
	}

	// Initialize keyshare manager
	mgr, err := keyshare.NewManager(home, *password)
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to create keyshare manager")
	}

	// Get private key (required)
	trimmedKey := strings.TrimSpace(*privateKeyHex)
	if trimmedKey == "" {
		logger.Fatal().Msg("private-key flag is required")
	}

	// Use provided private key
	privateKeyBase64, err := getHardcodedPrivateKey(trimmedKey)
	if err != nil {
		preview := trimmedKey
		if len(trimmedKey) > 16 {
			preview = trimmedKey[:16] + "..."
		}
		logger.Fatal().
			Err(err).
			Str("key_length", fmt.Sprintf("%d", len(trimmedKey))).
			Str("key_preview", preview).
			Msg("invalid private key provided")
	}
	logger.Info().Msg("using private key from command line flag")

	// Initialize libp2p transport
	tr, err := libp2ptransport.New(ctx, libp2ptransport.Config{
		ListenAddrs:      []string{*libp2pListen},
		ProtocolID:       "/tss/demo/1.0.0",
		PrivateKeyBase64: privateKeyBase64,
		DialTimeout:      10 * time.Second,
		IOTimeout:        15 * time.Second,
	}, logger)
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to start libp2p transport")
	}
	defer tr.Close()

	// Get listen addresses and peer ID
	listenAddrs := tr.ListenAddrs()
	peerID := tr.ID()
	if privateKeyBase64 != "" {
		logger.Info().
			Str("peer_id", peerID).
			Strs("addrs", listenAddrs).
			Msg("libp2p transport started (peer ID is deterministic from provided key)")
	} else {
		logger.Info().
			Str("peer_id", peerID).
			Strs("addrs", listenAddrs).
			Msg("libp2p transport started (peer ID is random - will change each run)")
	}

	// Setup database file for this node (each node has its own database)
	// In production, these databases are populated by on-chain event listening
	sanitized := strings.ReplaceAll(strings.ReplaceAll(*validatorAddr, ":", "_"), "/", "_")
	dbPath := fmt.Sprintf("/tmp/tss-%s.db", sanitized)
	database, err := db.OpenFileDB("/tmp", fmt.Sprintf("tss-%s.db", sanitized), true)
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to open database")
	}
	defer database.Close()
	logger.Info().Str("db_path", dbPath).Msg("using node-specific database")

	// Register this node in the shared registry file
	nodeInfo := nodeRegistryEntry{
		ValidatorAddress: *validatorAddr,
		PeerID:           peerID,
		Multiaddrs:       listenAddrs,
		LastUpdated:      time.Now(),
	}
	if err := registerNode(nodeInfo, logger); err != nil {
		logger.Fatal().Err(err).Msg("failed to register node in registry")
	}
	logger.Info().
		Str("peer_id", peerID).
		Strs("addrs", listenAddrs).
		Msg("registered node in shared registry")

	// Create simple data provider for demo
	dataProvider := &staticPushChainDataProvider{
		validatorAddress: *validatorAddr,
		logger:           logger,
	}

	// Initialize coordinator first (needed for EventStore)
	// We'll create a temporary coordinator to get EventStore, then create service with it
	// But we need service for coordinator... so we'll create coordinator with a nil service first
	// Actually, let's create coordinator properly - we need to pass a service, but we can update it later
	// Better approach: create a temporary service, then coordinator, then recreate service with EventStore
	// Actually, simplest: create coordinator with a dummy service reference, then create real service with EventStore

	// Create a temporary coordinator config to get the EventStore interface
	// But we need the coordinator to have the service... let's do it differently:
	// Create service first without EventStore, create coordinator, then update service's EventStore

	// Initialize coordinator (we'll update service reference later)
	coord, err := coordinator.NewCoordinator(coordinator.Config{
		DB:                database.Client(),
		Service:           nil, // Will be set after service creation
		DataProvider:      dataProvider,
		PartyID:           *validatorAddr,
		Logger:            logger,
		PollInterval:      500 * time.Millisecond,
		ProcessingTimeout: 2 * time.Minute,
		CoordinatorRange:  100,
	})
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to create coordinator")
	}

	// Initialize TSS core service with EventStore from coordinator
	service, err := core.NewService(core.Config{
		PartyID:        *validatorAddr,
		SetupTimeout:   30 * time.Second,
		MessageTimeout: 30 * time.Second,
		Logger:         logger,
	}, core.Dependencies{
		Transport:     tr,
		KeyshareStore: mgr,
		EventStore:    coord, // Coordinator implements EventStore
	})
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to create TSS service")
	}

	// Update coordinator with the service
	coord.SetService(service)

	// Start coordinator
	if err := coord.Start(ctx); err != nil {
		logger.Fatal().Err(err).Msg("failed to start coordinator")
	}
	defer coord.Stop()

	logger.Info().Msg("TSS demo node started and ready")

	// Print node info
	fmt.Printf("\n=== Node Info ===\n")
	fmt.Printf("Validator Address: %s\n", *validatorAddr)
	fmt.Printf("Peer ID: %s\n", tr.ID())
	fmt.Printf("Addresses: %v\n", listenAddrs)
	fmt.Printf("\nUse the CLI to trigger operations:\n")
	fmt.Printf("  ./build/tss keygen -node=%s -key-id=<key-id>\n", *validatorAddr)
	fmt.Printf("  ./build/tss keyrefresh -node=%s -key-id=<key-id>\n", *validatorAddr)
	fmt.Printf("  ./build/tss sign -node=%s -key-id=<key-id> -message=<message>\n", *validatorAddr)
	fmt.Printf("\nWaiting for events...\n\n")

	// Wait for shutdown
	<-ctx.Done()
	logger.Info().Msg("shutting down")
}

func runCommand(command string) {
	var (
		nodeID    = flag.String("node", "", "target node validator address")
		keyID     = flag.String("key-id", "", "key ID (required for keyrefresh and sign)")
		message   = flag.String("message", "hello world", "message to sign (for sign operation)")
		threshold = flag.Int("threshold", 2, "threshold for TSS")
		dbPath    = flag.String("db", "", "path to database file (defaults to /tmp/tss-<validator-address>.db)")
	)
	flag.Parse()

	if *nodeID == "" {
		fmt.Printf("node flag is required for %s command\n", command)
		flag.Usage()
		os.Exit(1)
	}

	logger := zerolog.New(zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: time.RFC3339}).
		With().
		Str("command", command).
		Str("validator", *nodeID).
		Timestamp().
		Logger()

	// For demo: populate events in all node databases
	// Read nodes from registry to find all database paths
	nodes, err := readNodeRegistry(logger)
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to read node registry")
	}

	nodeDBs := make([]string, 0, len(nodes))
	for _, node := range nodes {
		sanitized := strings.ReplaceAll(strings.ReplaceAll(node.ValidatorAddress, ":", "_"), "/", "_")
		nodeDBs = append(nodeDBs, fmt.Sprintf("/tmp/tss-%s.db", sanitized))
	}

	if len(nodeDBs) == 0 {
		logger.Fatal().Msg("no nodes found in registry - start at least one node first")
	}

	// If custom db path is provided, use only that one
	if *dbPath != "" {
		nodeDBs = []string{*dbPath}
	}

	blockNum := uint64(time.Now().Unix())

	// Create event structure
	var event store.TSSEvent
	switch command {
	case "keygen":
		if *keyID == "" {
			*keyID = fmt.Sprintf("demo-key-%d", time.Now().Unix())
		}
		eventData, _ := json.Marshal(map[string]interface{}{
			"key_id":    *keyID,
			"threshold": *threshold,
		})
		event = store.TSSEvent{
			EventID:      fmt.Sprintf("keygen-%d-%s", blockNum, *keyID),
			BlockNumber:  blockNum,
			ProtocolType: "keygen",
			Status:       "PENDING",
			ExpiryHeight: blockNum + 1000,
			EventData:    eventData,
		}

	case "keyrefresh":
		if *keyID == "" {
			logger.Fatal().Msg("key-id is required for keyrefresh")
		}
		eventData, _ := json.Marshal(map[string]interface{}{
			"key_id":    *keyID,
			"threshold": *threshold,
		})
		event = store.TSSEvent{
			EventID:      fmt.Sprintf("keyrefresh-%d-%s", blockNum, *keyID),
			BlockNumber:  blockNum,
			ProtocolType: "keyrefresh",
			Status:       "PENDING",
			ExpiryHeight: blockNum + 1000,
			EventData:    eventData,
		}

	case "sign":
		if *keyID == "" {
			logger.Fatal().Msg("key-id is required for sign")
		}
		hash := sha256.Sum256([]byte(*message))
		eventData, _ := json.Marshal(map[string]interface{}{
			"key_id":       *keyID,
			"threshold":    *threshold,
			"message_hash": hash[:],
			"chain_path":   []byte{},
		})
		event = store.TSSEvent{
			EventID:      fmt.Sprintf("sign-%d-%s", blockNum, *keyID),
			BlockNumber:  blockNum,
			ProtocolType: "sign",
			Status:       "PENDING",
			ExpiryHeight: blockNum + 1000,
			EventData:    eventData,
		}
	}

	// Write event to all node databases
	var successCount int
	var errors []string
	for _, dbPath := range nodeDBs {
		db, err := gorm.Open(sqlite.Open(dbPath+"?mode=rwc&cache=shared"), &gorm.Config{})
		if err != nil {
			errors = append(errors, fmt.Sprintf("%s: %v", dbPath, err))
			continue
		}

		if err := db.AutoMigrate(&store.TSSEvent{}); err != nil {
			errors = append(errors, fmt.Sprintf("%s (migrate): %v", dbPath, err))
			continue
		}

		// Create a copy of the event for this database
		eventCopy := event
		if err := db.Create(&eventCopy).Error; err != nil {
			errors = append(errors, fmt.Sprintf("%s (create): %v", dbPath, err))
		} else {
			successCount++
		}
	}

	if successCount == 0 {
		logger.Fatal().
			Strs("errors", errors).
			Msg("failed to create event in any database")
	}

	logger.Info().
		Str("event_id", event.EventID).
		Int("databases_updated", successCount).
		Int("total_databases", len(nodeDBs)).
		Msg("created event in node databases")

	if len(errors) > 0 {
		logger.Warn().
			Strs("errors", errors).
			Msg("some databases failed to update")
	}

	fmt.Printf("\nEvent created in %d/%d node databases!\n", successCount, len(nodeDBs))
	fmt.Println("The coordinators will pick it up and process it.")
	fmt.Println("Check the node logs to see the progress.")
}

// staticPushChainDataProvider implements PushChainDataProvider for demo/testing.
type staticPushChainDataProvider struct {
	validatorAddress string
	logger           zerolog.Logger
}

// GetLatestBlockNum implements coordinator.PushChainDataProvider.
func (p *staticPushChainDataProvider) GetLatestBlockNum(ctx context.Context) (uint64, error) {
	// Use timestamp as block number for demo
	return uint64(time.Now().Unix()), nil
}

// GetUniversalValidators implements coordinator.PushChainDataProvider.
func (p *staticPushChainDataProvider) GetUniversalValidators(ctx context.Context) ([]*tss.UniversalValidator, error) {
	// Read nodes from shared registry file
	nodes, err := readNodeRegistry(p.logger)
	if err != nil {
		return nil, fmt.Errorf("failed to read node registry: %w", err)
	}

	// Convert to UniversalValidator list
	validators := make([]*tss.UniversalValidator, 0, len(nodes))
	for _, node := range nodes {
		validators = append(validators, &tss.UniversalValidator{
			ValidatorAddress: node.ValidatorAddress,
			Status:           tss.UVStatusActive,
			Network: tss.NetworkInfo{
				PeerID:     node.PeerID,
				Multiaddrs: node.Multiaddrs,
			},
			JoinedAtBlock: 0,
		})
	}

	return validators, nil
}

// GetUniversalValidator implements coordinator.PushChainDataProvider.
func (p *staticPushChainDataProvider) GetUniversalValidator(ctx context.Context, validatorAddress string) (*tss.UniversalValidator, error) {
	validators, err := p.GetUniversalValidators(ctx)
	if err != nil {
		return nil, err
	}
	for _, v := range validators {
		if v.ValidatorAddress == validatorAddress {
			return v, nil
		}
	}
	return nil, fmt.Errorf("validator not found: %s", validatorAddress)
}

// getHardcodedPrivateKey converts a hex private key to base64-encoded libp2p format.
func getHardcodedPrivateKey(hexKey string) (string, error) {
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
