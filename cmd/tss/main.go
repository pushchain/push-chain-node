package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/pushchain/push-chain-node/universalClient/chains/push"
	"github.com/pushchain/push-chain-node/universalClient/db"
	"github.com/pushchain/push-chain-node/universalClient/pushcore"
	"github.com/pushchain/push-chain-node/universalClient/store"
	"github.com/pushchain/push-chain-node/universalClient/tss"
	"github.com/pushchain/push-chain-node/universalClient/tss/eventstore"
	"github.com/pushchain/push-chain-node/x/uvalidator/types"
)

const (
	// tssDataDir is the directory in project root for TSS data
	tssDataDir = "./tss-data"
	// nodesRegistryFile is the shared file where all nodes register themselves
	nodesRegistryFile = "./tss-data/tss-nodes.json"
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
	case "qc", "quorumchange":
		runQc()
	case "sign":
		runSign()
	case "prepare":
		runPrepare()
	case "status":
		runStatus()
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
	fmt.Println("  qc            Trigger a quorumchange operation")
	fmt.Println("  sign          Trigger a sign operation")
	fmt.Println("  prepare       Prepare environment (clean TSS files and build latest binary)")
	fmt.Println("  status        Set node status (active, pending_join, pending_leave, inactive)")
	fmt.Println("")
	fmt.Println("Examples:")
	fmt.Println("  tss node -validator-address=pushvaloper1... -private-key=30B0D9... -p2p-listen=/ip4/127.0.0.1/tcp/39001")
	fmt.Println("  tss keygen")
	fmt.Println("  tss keyrefresh")
	fmt.Println("  tss qc")
	fmt.Println("  tss sign -message=\"Hello, World!\"")
	fmt.Println("  tss prepare")
}

// nodeRegistry is the in-memory representation of the registry file
// Uses UniversalValidator structure directly
type nodeRegistry struct {
	Nodes []*types.UniversalValidator `json:"nodes"`
	mu    sync.RWMutex
}

var (
	registryMu sync.Mutex
)

// registerNode adds or updates a node in the shared registry file
func registerNode(node *types.UniversalValidator, logger zerolog.Logger) error {
	registryMu.Lock()
	defer registryMu.Unlock()

	// Read existing registry
	registry := &nodeRegistry{Nodes: []*types.UniversalValidator{}}
	data, err := os.ReadFile(nodesRegistryFile)
	if err == nil {
		if err := json.Unmarshal(data, registry); err != nil {
			logger.Warn().Err(err).Msg("failed to parse existing registry, creating new one")
			registry.Nodes = []*types.UniversalValidator{}
		}
	}

	// Update or add node
	found := false
	nodeAddr := ""
	if node.IdentifyInfo != nil {
		nodeAddr = node.IdentifyInfo.CoreValidatorAddress
	}
	for i := range registry.Nodes {
		registryAddr := ""
		if registry.Nodes[i].IdentifyInfo != nil {
			registryAddr = registry.Nodes[i].IdentifyInfo.CoreValidatorAddress
		}
		if registryAddr == nodeAddr {
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
func readNodeRegistry(logger zerolog.Logger) ([]*types.UniversalValidator, error) {
	registryMu.Lock()
	defer registryMu.Unlock()

	data, err := os.ReadFile(nodesRegistryFile)
	if err != nil {
		if os.IsNotExist(err) {
			// File doesn't exist yet, return empty list
			return []*types.UniversalValidator{}, nil
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
		homeDir       = flag.String("home", "", "directory for keyshare storage (defaults to ./tss-data/tss-<validator>)")
		password      = flag.String("password", "demo-password", "encryption password for keyshares")
		dbPath        = flag.String("db", "", "database file path (defaults to ./tss-data/tss-<validator>/uv.db)")
		grpcURL       = flag.String("grpc", "", "Push Chain gRPC URL (e.g., localhost:9090). If not provided, uses static demo provider")
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

	// Ensure tss-data directory exists
	if err := os.MkdirAll(tssDataDir, 0755); err != nil {
		fmt.Printf("failed to create tss-data directory: %v\n", err)
		os.Exit(1)
	}

	// Set defaults for home and db if not provided
	if *homeDir == "" {
		sanitized := strings.ReplaceAll(strings.ReplaceAll(*validatorAddr, ":", "_"), "/", "_")
		*homeDir = filepath.Join(tssDataDir, fmt.Sprintf("tss-%s", sanitized))
	}
	if *dbPath == "" {
		sanitized := strings.ReplaceAll(strings.ReplaceAll(*validatorAddr, ":", "_"), "/", "_")
		// Database is stored as uv.db inside the node's directory
		nodeDir := filepath.Join(tssDataDir, fmt.Sprintf("tss-%s", sanitized))
		if err := os.MkdirAll(nodeDir, 0755); err != nil {
			fmt.Printf("failed to create node directory: %v\n", err)
			os.Exit(1)
		}
		*dbPath = filepath.Join(nodeDir, "uv.db")
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

	// Create pushcore client - required for TSS node
	var pushClient *pushcore.Client
	var tssListener *push.PushTSSEventListener
	if *grpcURL != "" {
		// Create pushcore client directly
		var err error
		pushClient, err = pushcore.New([]string{*grpcURL}, logger)
		if err != nil {
			logger.Fatal().Err(err).Str("grpc_url", *grpcURL).Msg("failed to create pushcore client")
		}
		defer pushClient.Close()
		logger.Info().Str("grpc_url", *grpcURL).Msg("using pushcore client (connected to blockchain)")

		// Create event store from database
		evtStore := eventstore.NewStore(database.Client(), logger)

		// Create and start the PushTSSEventListener to poll blockchain events
		tssListener = push.NewPushTSSEventListener(pushClient, evtStore, logger)
		if err := tssListener.Start(ctx); err != nil {
			logger.Fatal().Err(err).Msg("failed to start TSS event listener")
		}
		defer tssListener.Stop()
		logger.Info().Msg("started Push TSS event listener")
	} else {
		// For demo mode, we still need a pushcore client, but we'll use static data
		// Create a dummy client (this won't work for real operations, but allows the code to compile)
		// In practice, grpcURL should always be provided
		logger.Warn().Msg("no gRPC URL provided - TSS node requires pushcore client")
		// We'll need to handle this case - for now, let's require grpcURL
		logger.Fatal().Msg("gRPC URL is required for TSS node")
	}

	// Initialize TSS node
	tssNode, err := tss.NewNode(ctx, tss.Config{
		ValidatorAddress:  *validatorAddr,
		P2PPrivateKeyHex:  strings.TrimSpace(*privateKeyHex),
		LibP2PListen:      *libp2pListen,
		HomeDir:           *homeDir,
		Password:          *password,
		Database:          database,
		PushCore:          pushClient,
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
	// Default status is pending_join for new nodes
	nodeInfo := &types.UniversalValidator{
		IdentifyInfo: &types.IdentityInfo{
			CoreValidatorAddress: *validatorAddr,
		},
		NetworkInfo: &types.NetworkInfo{
			PeerId:     peerID,
			MultiAddrs: listenAddrs,
		},
		LifecycleInfo: &types.LifecycleInfo{
			CurrentStatus: types.UVStatus_UV_STATUS_PENDING_JOIN, // Default status for new nodes
		},
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
		nodeAddr := ""
		if node.IdentifyInfo != nil {
			nodeAddr = node.IdentifyInfo.CoreValidatorAddress
		}
		sanitized := strings.ReplaceAll(strings.ReplaceAll(nodeAddr, ":", "_"), "/", "_")
		// Database is stored as uv.db inside the node's directory
		nodeDBs = append(nodeDBs, filepath.Join(tssDataDir, fmt.Sprintf("tss-%s", sanitized), "uv.db"))
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
		nodeAddr := ""
		if node.IdentifyInfo != nil {
			nodeAddr = node.IdentifyInfo.CoreValidatorAddress
		}
		sanitized := strings.ReplaceAll(strings.ReplaceAll(nodeAddr, ":", "_"), "/", "_")
		// Database is stored as uv.db inside the node's directory
		nodeDBs = append(nodeDBs, filepath.Join(tssDataDir, fmt.Sprintf("tss-%s", sanitized), "uv.db"))
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

func runQc() {
	flag.Parse()

	logger := zerolog.New(zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: time.RFC3339}).
		With().
		Str("command", "qc").
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
		nodeAddr := ""
		if node.IdentifyInfo != nil {
			nodeAddr = node.IdentifyInfo.CoreValidatorAddress
		}
		sanitized := strings.ReplaceAll(strings.ReplaceAll(nodeAddr, ":", "_"), "/", "_")
		// Database is stored as uv.db inside the node's directory
		nodeDBs = append(nodeDBs, filepath.Join(tssDataDir, fmt.Sprintf("tss-%s", sanitized), "uv.db"))
	}

	blockNum := uint64(time.Now().Unix())
	eventID := fmt.Sprintf("qc-%d", blockNum)

	// Create event in all node databases
	// eventData is empty for quorumchange
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
			ProtocolType: "quorumchange",
			Status:       "PENDING",
			EventData:    nil,             // Empty for quorumchange
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
			Msg("created quorumchange event in database")
	}

	logger.Info().
		Str("event_id", eventID).
		Int("success", successCount).
		Int("total", len(nodes)).
		Msg("quorumchange event creation completed")
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
		nodeAddr := ""
		if node.IdentifyInfo != nil {
			nodeAddr = node.IdentifyInfo.CoreValidatorAddress
		}
		sanitized := strings.ReplaceAll(strings.ReplaceAll(nodeAddr, ":", "_"), "/", "_")
		// Database is stored as uv.db inside the node's directory
		nodeDBs = append(nodeDBs, filepath.Join(tssDataDir, fmt.Sprintf("tss-%s", sanitized), "uv.db"))
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

func runPrepare() {
	flag.Parse()

	logger := zerolog.New(zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: time.RFC3339}).
		With().
		Str("command", "prepare").
		Timestamp().
		Logger()

	logger.Info().Msg("preparing environment: cleaning TSS files and building binary")

	// Step 1: Clean TSS files
	logger.Info().Str("dir", tssDataDir).Msg("cleaning all TSS-related files")

	// Clean node registry file
	if err := os.Remove(nodesRegistryFile); err != nil {
		if !os.IsNotExist(err) {
			logger.Warn().Err(err).Str("file", nodesRegistryFile).Msg("failed to remove node registry file")
		} else {
			logger.Debug().Str("file", nodesRegistryFile).Msg("node registry file does not exist")
		}
	} else {
		logger.Info().Str("file", nodesRegistryFile).Msg("removed node registry file")
	}

	// Find and remove all TSS directories and database files
	entries, err := os.ReadDir(tssDataDir)
	if err != nil {
		if os.IsNotExist(err) {
			logger.Info().Str("dir", tssDataDir).Msg("tss-data directory does not exist, nothing to clean")
		} else {
			logger.Fatal().Err(err).Str("dir", tssDataDir).Msg("failed to read tss-data directory")
		}
	} else {
		removedDirs := 0
		removedDBs := 0

		for _, entry := range entries {
			name := entry.Name()

			// Remove TSS directories (tss-*)
			if strings.HasPrefix(name, "tss-") && entry.IsDir() {
				dirPath := filepath.Join(tssDataDir, name)
				if err := os.RemoveAll(dirPath); err != nil {
					logger.Warn().Err(err).Str("dir", dirPath).Msg("failed to remove TSS directory")
				} else {
					logger.Info().Str("dir", dirPath).Msg("removed TSS directory")
					removedDirs++
				}
			}

			// Remove uv.db files inside tss-* directories
			if strings.HasPrefix(name, "tss-") && entry.IsDir() {
				dbPath := filepath.Join(tssDataDir, name, "uv.db")
				if _, err := os.Stat(dbPath); err == nil {
					if err := os.Remove(dbPath); err != nil {
						logger.Warn().Err(err).Str("file", dbPath).Msg("failed to remove TSS database file")
					} else {
						logger.Info().Str("file", dbPath).Msg("removed TSS database file")
						removedDBs++
					}
				}
			}
		}

		logger.Info().
			Int("directories_removed", removedDirs).
			Int("database_files_removed", removedDBs).
			Msg("clean completed")
	}

	// Step 2: Build latest binary
	logger.Info().Msg("building latest binary...")

	// Ensure build directory exists
	if _, err := os.Stat("./build"); os.IsNotExist(err) {
		if err := os.MkdirAll("./build", 0755); err != nil {
			logger.Warn().Err(err).Msg("failed to create build directory, continuing anyway")
		}
	}

	// Execute build command
	cmd := exec.Command("go", "build", "-o", "./build/tss", "./cmd/tss")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		logger.Fatal().Err(err).Msg("failed to build binary")
	}

	logger.Info().
		Str("binary", "./build/tss").
		Msg("prepare completed successfully")
}

func runStatus() {
	var (
		validatorAddr = flag.String("validator-address", "", "validator address (required)")
		status        = flag.String("status", "", "status to set: active, pending_join, pending_leave, inactive (required)")
	)
	flag.Parse()

	if *validatorAddr == "" {
		fmt.Println("validator-address flag is required")
		flag.Usage()
		os.Exit(1)
	}

	if *status == "" {
		fmt.Println("status flag is required")
		flag.Usage()
		os.Exit(1)
	}

	// Validate status
	validStatuses := map[string]bool{
		"active":        true,
		"pending_join":  true,
		"pending_leave": true,
		"inactive":      true,
	}
	if !validStatuses[*status] {
		fmt.Printf("Invalid status: %s. Must be one of: active, pending_join, pending_leave, inactive\n", *status)
		os.Exit(1)
	}

	logger := zerolog.New(zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: time.RFC3339}).
		With().
		Str("command", "status").
		Timestamp().
		Logger()

	// Read existing registry
	registryMu.Lock()
	defer registryMu.Unlock()

	registry := &nodeRegistry{Nodes: []*types.UniversalValidator{}}
	data, err := os.ReadFile(nodesRegistryFile)
	if err == nil {
		if err := json.Unmarshal(data, registry); err != nil {
			logger.Warn().Err(err).Msg("failed to parse existing registry, creating new one")
			registry.Nodes = []*types.UniversalValidator{}
		}
	}

	// Map status string to UVStatus
	var uvStatus types.UVStatus
	switch *status {
	case "active":
		uvStatus = types.UVStatus_UV_STATUS_ACTIVE
	case "pending_join":
		uvStatus = types.UVStatus_UV_STATUS_PENDING_JOIN
	case "pending_leave":
		uvStatus = types.UVStatus_UV_STATUS_PENDING_LEAVE
	case "inactive":
		uvStatus = types.UVStatus_UV_STATUS_INACTIVE
	default:
		logger.Fatal().Str("status", *status).Msg("invalid status")
	}

	// Find and update the node
	found := false
	for i := range registry.Nodes {
		nodeAddr := ""
		if registry.Nodes[i].IdentifyInfo != nil {
			nodeAddr = registry.Nodes[i].IdentifyInfo.CoreValidatorAddress
		}
		if nodeAddr == *validatorAddr {
			if registry.Nodes[i].LifecycleInfo == nil {
				registry.Nodes[i].LifecycleInfo = &types.LifecycleInfo{}
			}
			registry.Nodes[i].LifecycleInfo.CurrentStatus = uvStatus
			found = true
			logger.Info().
				Str("validator", *validatorAddr).
				Str("status", *status).
				Msg("updated node status")
			break
		}
	}

	if !found {
		logger.Fatal().
			Str("validator", *validatorAddr).
			Msg("node not found in registry - start the node first")
	}

	// Write back to file
	data, err = json.MarshalIndent(registry, "", "  ")
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to marshal registry")
	}

	if err := os.WriteFile(nodesRegistryFile, data, 0644); err != nil {
		logger.Fatal().Err(err).Msg("failed to write registry file")
	}

	logger.Info().
		Str("validator", *validatorAddr).
		Str("status", *status).
		Msg("status updated successfully")
}
