package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/pushchain/push-chain-node/universalClient/db"
	"github.com/pushchain/push-chain-node/universalClient/store"
	"github.com/pushchain/push-chain-node/universalClient/tss"
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
	case "keygen":
		runKeygen()
	case "keyrefresh":
		runKeyrefresh()
	case "sign":
		runSign()
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
	fmt.Println("  tss node -validator-address=pushvaloper1... -private-key=30B0D9... -p2p-listen=/ip4/127.0.0.1/tcp/39001")
	fmt.Println("  tss keygen")
	fmt.Println("  tss keyrefresh")
	fmt.Println("  tss sign -message=\"Hello, World!\"")
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
		homeDir       = flag.String("home", "", "directory for keyshare storage (defaults to /tmp/tss-<validator>)")
		password      = flag.String("password", "demo-password", "encryption password for keyshares")
		dbPath        = flag.String("db", "", "database file path (defaults to /tmp/tss-<validator>.db)")
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

	// Set defaults for home and db if not provided
	if *homeDir == "" {
		sanitized := strings.ReplaceAll(strings.ReplaceAll(*validatorAddr, ":", "_"), "/", "_")
		*homeDir = fmt.Sprintf("/tmp/tss-%s", sanitized)
	}
	if *dbPath == "" {
		sanitized := strings.ReplaceAll(strings.ReplaceAll(*validatorAddr, ":", "_"), "/", "_")
		*dbPath = fmt.Sprintf("/tmp/tss-%s.db", sanitized)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	logger := zerolog.New(zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: time.RFC3339}).
		With().
		Str("validator", *validatorAddr).
		Timestamp().
		Logger()

	// Create database (extract dir and filename from dbPath)
	dbDir := filepath.Dir(*dbPath)
	dbFilename := filepath.Base(*dbPath)
	database, err := db.OpenFileDB(dbDir, dbFilename, true)
	if err != nil {
		logger.Fatal().Err(err).Str("db_path", *dbPath).Msg("failed to open database")
	}
	defer database.Close()

	// Create simple data provider for demo
	dataProvider := NewStaticPushChainDataProvider(*validatorAddr, logger)

	// Initialize TSS node
	tssNode, err := tss.NewNode(ctx, tss.Config{
		ValidatorAddress:  *validatorAddr,
		PrivateKeyHex:     strings.TrimSpace(*privateKeyHex),
		LibP2PListen:      *libp2pListen,
		HomeDir:           *homeDir,
		Password:          *password,
		Database:          database,
		DataProvider:      dataProvider,
		Logger:            logger,
		PollInterval:      2 * time.Second,
		ProcessingTimeout: 2 * time.Minute,
		CoordinatorRange:  1000,
		ProtocolID:        "/tss/demo/1.0.0",
		DialTimeout:       10 * time.Second,
		IOTimeout:         15 * time.Second,
	})
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to create TSS node")
	}
	defer tssNode.Stop()

	// Start the TSS node (network must be started before we can get peer ID and addresses)
	if err := tssNode.Start(ctx); err != nil {
		logger.Fatal().Err(err).Msg("failed to start TSS node")
	}

	// Get listen addresses and peer ID for registry (after network is started)
	listenAddrs := tssNode.ListenAddrs()
	peerID := tssNode.PeerID()

	if peerID == "" {
		logger.Warn().Msg("peer ID is empty, node may not be properly registered")
	}

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
		Strs("multiaddrs", listenAddrs).
		Msg("node registered in registry")

	// Wait for shutdown
	<-ctx.Done()
	logger.Info().Msg("shutting down")
}

func runKeygen() {
	flag.Parse()

	logger := zerolog.New(zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: time.RFC3339}).
		With().
		Str("command", "keygen").
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
	eventID := fmt.Sprintf("keygen-%d", blockNum)

	// Create event in all node databases
	// eventData is empty for keygen (keyID will be generated by DKLS)
	successCount := 0
	for _, dbPath := range nodeDBs {
		db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
		if err != nil {
			logger.Warn().Err(err).Str("db", dbPath).Msg("failed to open database, skipping")
			continue
		}

		// Auto-migrate
		if err := db.AutoMigrate(&store.TSSEvent{}); err != nil {
			logger.Warn().Err(err).Str("db", dbPath).Msg("failed to migrate database, skipping")
			continue
		}

		event := store.TSSEvent{
			EventID:      eventID,
			BlockNumber:  blockNum,
			ProtocolType: "keygen",
			Status:       "PENDING",
			EventData:    nil,             // Empty for keygen
			ExpiryHeight: blockNum + 1000, // Expire after 1000 blocks
		}

		if err := db.Create(&event).Error; err != nil {
			logger.Warn().Err(err).Str("db", dbPath).Msg("failed to create event, skipping")
			continue
		}

		successCount++
		logger.Info().
			Str("event_id", eventID).
			Uint64("block", blockNum).
			Str("db", dbPath).
			Msg("created keygen event in database")
	}

	logger.Info().
		Str("event_id", eventID).
		Int("success", successCount).
		Int("total", len(nodes)).
		Msg("keygen event creation completed")
}

func runKeyrefresh() {
	flag.Parse()

	logger := zerolog.New(zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: time.RFC3339}).
		With().
		Str("command", "keyrefresh").
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
	eventID := fmt.Sprintf("keyrefresh-%d", blockNum)

	// Create event in all node databases
	// eventData is empty for keyrefresh
	successCount := 0
	for _, dbPath := range nodeDBs {
		db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
		if err != nil {
			logger.Warn().Err(err).Str("db", dbPath).Msg("failed to open database, skipping")
			continue
		}

		// Auto-migrate
		if err := db.AutoMigrate(&store.TSSEvent{}); err != nil {
			logger.Warn().Err(err).Str("db", dbPath).Msg("failed to migrate database, skipping")
			continue
		}

		event := store.TSSEvent{
			EventID:      eventID,
			BlockNumber:  blockNum,
			ProtocolType: "keyrefresh",
			Status:       "PENDING",
			EventData:    nil,             // Empty for keyrefresh
			ExpiryHeight: blockNum + 1000, // Expire after 1000 blocks
		}

		if err := db.Create(&event).Error; err != nil {
			logger.Warn().Err(err).Str("db", dbPath).Msg("failed to create event, skipping")
			continue
		}

		successCount++
		logger.Info().
			Str("event_id", eventID).
			Uint64("block", blockNum).
			Str("db", dbPath).
			Msg("created keyrefresh event in database")
	}

	logger.Info().
		Str("event_id", eventID).
		Int("success", successCount).
		Int("total", len(nodes)).
		Msg("keyrefresh event creation completed")
}

func runSign() {
	var (
		message = flag.String("message", "", "message to sign (required)")
	)
	flag.Parse()

	if *message == "" {
		fmt.Println("message flag is required for sign command")
		flag.Usage()
		os.Exit(1)
	}

	logger := zerolog.New(zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: time.RFC3339}).
		With().
		Str("command", "sign").
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
	eventID := fmt.Sprintf("sign-%d", blockNum)

	// Create event data with message
	eventData, _ := json.Marshal(map[string]string{
		"message": *message,
	})

	// Create event in all node databases
	successCount := 0
	for _, dbPath := range nodeDBs {
		db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
		if err != nil {
			logger.Warn().Err(err).Str("db", dbPath).Msg("failed to open database, skipping")
			continue
		}

		// Auto-migrate
		if err := db.AutoMigrate(&store.TSSEvent{}); err != nil {
			logger.Warn().Err(err).Str("db", dbPath).Msg("failed to migrate database, skipping")
			continue
		}

		event := store.TSSEvent{
			EventID:      eventID,
			BlockNumber:  blockNum,
			ProtocolType: "sign",
			Status:       "PENDING",
			EventData:    eventData,
			ExpiryHeight: blockNum + 1000, // Expire after 1000 blocks
		}

		if err := db.Create(&event).Error; err != nil {
			logger.Warn().Err(err).Str("db", dbPath).Msg("failed to create event, skipping")
			continue
		}

		successCount++
		logger.Info().
			Str("event_id", eventID).
			Str("message", *message).
			Uint64("block", blockNum).
			Str("db", dbPath).
			Msg("created sign event in database")
	}

	logger.Info().
		Str("event_id", eventID).
		Str("message", *message).
		Int("success", successCount).
		Int("total", len(nodes)).
		Msg("sign event creation completed")
}
