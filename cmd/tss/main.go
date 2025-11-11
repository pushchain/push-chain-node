package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

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
	fmt.Println("  tss node -party=party-1 -p2p-listen=/ip4/127.0.0.1/tcp/39001")
	fmt.Println("  tss keygen -node=party-1 -key-id=demo-key-1")
	fmt.Println("  tss keyrefresh -node=party-1 -key-id=demo-key-1")
	fmt.Println("  tss sign -node=party-1 -key-id=demo-key-1 -message='hello'")
}

func runNode() {
	var (
		partyID      = flag.String("party", "", "party identifier (unique per node, e.g., party-1)")
		libp2pListen = flag.String("p2p-listen", "/ip4/127.0.0.1/tcp/0", "libp2p listen multiaddr")
		homeDir      = flag.String("home", "", "directory for keyshare storage (defaults to temp)")
		password     = flag.String("password", "demo-password", "encryption password for keyshares")
		peersFile    = flag.String("peers", "", "JSON file with peer information (for demo)")
	)
	flag.Parse()

	if *partyID == "" {
		fmt.Println("party flag is required")
		flag.Usage()
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	logger := zerolog.New(zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: time.RFC3339}).
		With().
		Str("party", *partyID).
		Timestamp().
		Logger()

	// Setup home directory
	home := *homeDir
	if home == "" {
		tmp, err := os.MkdirTemp("", "tss-demo-"+*partyID+"-")
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

	// Initialize libp2p transport
	tr, err := libp2ptransport.New(ctx, libp2ptransport.Config{
		ListenAddrs: []string{*libp2pListen},
		ProtocolID:  "/tss/demo/1.0.0",
		DialTimeout: 10 * time.Second,
		IOTimeout:   15 * time.Second,
	}, logger)
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to start libp2p transport")
	}
	defer tr.Close()

	// Get listen addresses
	listenAddrs := tr.ListenAddrs()
	logger.Info().
		Str("peer_id", tr.ID()).
		Strs("addrs", listenAddrs).
		Msg("libp2p transport started")

	// Setup database file for this node (each node has its own database)
	// In production, these databases are populated by on-chain event listening
	dbPath := fmt.Sprintf("/tmp/tss-%s.db", *partyID)
	database, err := db.OpenFileDB("/tmp", fmt.Sprintf("tss-%s.db", *partyID), true)
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to open database")
	}
	defer database.Close()
	logger.Info().Str("db_path", dbPath).Msg("using node-specific database")

	// Write this node's info to shared file (one file per party)
	peerInfoFile := "/tmp/tss-demo-peers.json"
	partyPeerFile := fmt.Sprintf("%s.%s", peerInfoFile, *partyID)
	if err := writePeerInfo(partyPeerFile, *partyID, tr.ID(), listenAddrs, logger); err != nil {
		logger.Warn().Err(err).Msg("failed to write peer info, continuing anyway")
	}

	// Load or create participant information
	// Wait a bit for other nodes to write their peer info
	time.Sleep(2 * time.Second)

	participantProvider, err := loadParticipantProvider(*peersFile, peerInfoFile, *partyID, tr.ID(), listenAddrs, logger)
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to load participant provider")
	}

	// Periodically refresh participant info to pick up new nodes
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				updated, err := loadParticipantProvider(*peersFile, peerInfoFile, *partyID, tr.ID(), listenAddrs, logger)
				if err == nil {
					// Update provider with latest peer info
					for protocol := range map[string]bool{"": true, "keygen": true, "keyrefresh": true, "sign": true} {
						participants, _ := updated.GetParticipants(ctx, protocol, 0)
						participantProvider.SetParticipants(protocol, participants)
					}
				}
			}
		}
	}()

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
		DB:                  database.Client(),
		Service:             nil, // Will be set after service creation
		ParticipantProvider: participantProvider,
		PartyID:             *partyID,
		BlockNumberGetter: func() uint64 {
			return uint64(time.Now().Unix()) // Use timestamp as block number for demo
		},
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
		NodeID:         *partyID,
		PartyID:        *partyID,
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
	fmt.Printf("Party ID: %s\n", *partyID)
	fmt.Printf("Peer ID: %s\n", tr.ID())
	fmt.Printf("Addresses: %v\n", listenAddrs)
	fmt.Printf("\nUse the CLI to trigger operations:\n")
	fmt.Printf("  ./build/tss keygen -node=%s -key-id=<key-id>\n", *partyID)
	fmt.Printf("  ./build/tss keyrefresh -node=%s -key-id=<key-id>\n", *partyID)
	fmt.Printf("  ./build/tss sign -node=%s -key-id=<key-id> -message=<message>\n", *partyID)
	fmt.Printf("\nWaiting for events...\n\n")

	// Wait for shutdown
	<-ctx.Done()
	logger.Info().Msg("shutting down")
}

func runCommand(command string) {
	var (
		nodeID    = flag.String("node", "", "target node party ID (e.g., party-1)")
		keyID     = flag.String("key-id", "", "key ID (required for keyrefresh and sign)")
		message   = flag.String("message", "hello world", "message to sign (for sign operation)")
		threshold = flag.Int("threshold", 2, "threshold for TSS")
		dbPath    = flag.String("db", "", "path to database file (defaults to /tmp/tss-<node-id>.db)")
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
		Str("node", *nodeID).
		Timestamp().
		Logger()

	// For demo: populate events in all node databases
	// In production, each node would listen to on-chain events and populate its own database
	nodeDBs := []string{
		fmt.Sprintf("/tmp/tss-party-1.db"),
		fmt.Sprintf("/tmp/tss-party-2.db"),
		fmt.Sprintf("/tmp/tss-party-3.db"),
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

// writePeerInfo writes this node's peer information to a shared file.
func writePeerInfo(filename, partyID, peerID string, addrs []string, logger zerolog.Logger) error {
	type peerInfo struct {
		PartyID    string   `json:"party_id"`
		PeerID     string   `json:"peer_id"`
		Multiaddrs []string `json:"multiaddrs"`
		UpdatedAt  int64    `json:"updated_at"`
	}

	info := peerInfo{
		PartyID:    partyID,
		PeerID:     peerID,
		Multiaddrs: addrs,
		UpdatedAt:  time.Now().Unix(),
	}

	data, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return err
	}

	// Use file locking or atomic write
	tmpFile := filename + ".tmp"
	if err := os.WriteFile(tmpFile, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmpFile, filename)
}

// loadAllPeerInfo loads peer information from the shared file.
func loadAllPeerInfo(filename string) (map[string]tss.Participant, error) {
	type peerInfo struct {
		PartyID    string   `json:"party_id"`
		PeerID     string   `json:"peer_id"`
		Multiaddrs []string `json:"multiaddrs"`
	}

	peers := make(map[string]tss.Participant)

	// Read all peer info files (party-1.json, party-2.json, party-3.json)
	for i := 1; i <= 3; i++ {
		partyID := fmt.Sprintf("party-%d", i)
		file := fmt.Sprintf("%s.%s", filename, partyID)

		data, err := os.ReadFile(file)
		if err != nil {
			if os.IsNotExist(err) {
				continue // Node not started yet
			}
			return nil, err
		}

		var info peerInfo
		if err := json.Unmarshal(data, &info); err != nil {
			continue // Skip invalid entries
		}

		peers[info.PartyID] = tss.Participant{
			PartyID:    info.PartyID,
			PeerID:     info.PeerID,
			Multiaddrs: info.Multiaddrs,
		}
	}

	return peers, nil
}

// loadParticipantProvider loads or creates participant information for the demo.
func loadParticipantProvider(peersFile, sharedPeerFile, partyID, peerID string, addrs []string, logger zerolog.Logger) (*coordinator.StaticParticipantProvider, error) {
	provider := coordinator.NewStaticParticipantProvider(logger)

	if peersFile != "" {
		// Load from explicit file
		data, err := os.ReadFile(peersFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read peers file: %w", err)
		}

		var peers map[string][]tss.Participant
		if err := json.Unmarshal(data, &peers); err != nil {
			return nil, fmt.Errorf("failed to parse peers file: %w", err)
		}

		for protocol, participants := range peers {
			provider.SetParticipants(protocol, participants)
		}
		logger.Info().Str("file", peersFile).Msg("loaded participants from file")
	} else {
		// Try to load from shared peer info files
		allPeers, err := loadAllPeerInfo(sharedPeerFile)
		if err != nil {
			logger.Debug().Err(err).Msg("failed to load shared peer info")
		}

		// Create default participants for 3-node demo
		participants := []tss.Participant{
			{
				PartyID:    "party-1",
				PeerID:     "unknown",
				Multiaddrs: []string{"/ip4/127.0.0.1/tcp/39001"},
			},
			{
				PartyID:    "party-2",
				PeerID:     "unknown",
				Multiaddrs: []string{"/ip4/127.0.0.1/tcp/39002"},
			},
			{
				PartyID:    "party-3",
				PeerID:     "unknown",
				Multiaddrs: []string{"/ip4/127.0.0.1/tcp/39003"},
			},
		}

		// Update with known peer info
		for i := range participants {
			if p, ok := allPeers[participants[i].PartyID]; ok {
				participants[i].PeerID = p.PeerID
				participants[i].Multiaddrs = p.Multiaddrs
			} else if participants[i].PartyID == partyID {
				// Use this node's actual info
				participants[i].PeerID = peerID
				participants[i].Multiaddrs = addrs
			}
		}

		provider.SetParticipants("", participants) // Default for all protocols
		logger.Info().
			Str("local_party", partyID).
			Str("local_peer_id", peerID).
			Int("known_peers", len(allPeers)).
			Msg("loaded participant configuration")
	}

	return provider, nil
}
