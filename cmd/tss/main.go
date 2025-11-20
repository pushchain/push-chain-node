package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/pushchain/push-chain-node/universalClient/store"
	"github.com/pushchain/push-chain-node/universalClient/tss/node"
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
	fmt.Println("  tss keygen -key-id=demo-key-1")
	fmt.Println("  tss keyrefresh -key-id=demo-key-1")
	fmt.Println("  tss sign -key-id=demo-key-1")
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
		privateKeyHex = flag.String("private-key", "", "Ed25519 private key in hex format (required)")
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

	if *privateKeyHex == "" {
		fmt.Println("private-key flag is required")
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

	// Create simple data provider for demo
	dataProvider := NewStaticPushChainDataProvider(*validatorAddr, logger)

	// Initialize TSS node
	tssNode, err := node.NewNode(ctx, node.Config{
		ValidatorAddress:  *validatorAddr,
		PrivateKeyHex:     strings.TrimSpace(*privateKeyHex),
		LibP2PListen:      *libp2pListen,
		HomeDir:           *homeDir,
		Password:          *password,
		Database:          nil, // Will create default database
		DataProvider:      dataProvider,
		Logger:            logger,
		PollInterval:      500 * time.Millisecond,
		ProcessingTimeout: 2 * time.Minute,
		CoordinatorRange:  100,
		SetupTimeout:      30 * time.Second,
		MessageTimeout:    30 * time.Second,
		ProtocolID:        "/tss/demo/1.0.0",
		DialTimeout:       10 * time.Second,
		IOTimeout:         15 * time.Second,
	})
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to create TSS node")
	}
	defer tssNode.Stop()

	// Get listen addresses and peer ID for registry
	listenAddrs := tssNode.ListenAddrs()
	peerID := tssNode.PeerID()

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
	// Start the TSS node
	if err := tssNode.Start(ctx); err != nil {
		logger.Fatal().Err(err).Msg("failed to start TSS node")
	}

	// Wait for shutdown
	<-ctx.Done()
	logger.Info().Msg("shutting down")
}

func runCommand(command string) {
	var (
		keyID = flag.String("key-id", "", "key ID (required)")
	)
	flag.Parse()

	logger := zerolog.New(zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: time.RFC3339}).
		With().
		Str("command", command).
		Timestamp().
		Logger()

	// Read all nodes from registry
	nodes, err := readNodeRegistry(logger)
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to read node registry")
	}

	if len(nodes) == 0 {
		logger.Fatal().Msg("no nodes found in registry - start at least one node first")
	}

	// Get all database paths
	nodeDBs := make([]string, 0, len(nodes))
	for _, node := range nodes {
		sanitized := strings.ReplaceAll(strings.ReplaceAll(node.ValidatorAddress, ":", "_"), "/", "_")
		nodeDBs = append(nodeDBs, fmt.Sprintf("/tmp/tss-%s.db", sanitized))
	}

	blockNum := uint64(time.Now().Unix())

	// Generate key ID if not provided (only for keygen)
	if *keyID == "" {
		if command == "keygen" {
			*keyID = fmt.Sprintf("demo-key-%d", time.Now().Unix())
		} else {
			logger.Fatal().Msg("key-id is required")
		}
	}

	// Create event data based on command type
	var eventData []byte
	var protocolType string
	var eventIDPrefix string

	switch command {
	case "keygen":
		eventData, _ = json.Marshal(map[string]interface{}{
			"key_id": *keyID,
		})
		protocolType = "keygen"
		eventIDPrefix = "keygen"
	case "keyrefresh":
		eventData, _ = json.Marshal(map[string]interface{}{
			"key_id": *keyID,
		})
		protocolType = "keyrefresh"
		eventIDPrefix = "keyrefresh"
	case "sign":
		hash := sha256.Sum256([]byte("hello world")) // Simple default message
		eventData, _ = json.Marshal(map[string]interface{}{
			"key_id":       *keyID,
			"message_hash": hash[:],
			"chain_path":   []byte{},
		})
		protocolType = "sign"
		eventIDPrefix = "sign"
	default:
		logger.Fatal().Msgf("unknown command: %s", command)
	}

	event := store.TSSEvent{
		EventID:      fmt.Sprintf("%s-%d-%s", eventIDPrefix, blockNum, *keyID),
		BlockNumber:  blockNum,
		ProtocolType: protocolType,
		Status:       "PENDING",
		ExpiryHeight: blockNum + 1000,
		EventData:    eventData,
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
}
