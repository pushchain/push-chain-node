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
		peerIDs      = flag.String("peer-ids", "", "comma-separated peer IDs in format party-1:peerid1,party-2:peerid2")
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
	peerID := tr.ID()
	logger.Info().
		Str("peer_id", peerID).
		Strs("addrs", listenAddrs).
		Msg("libp2p transport started")

	// Write peer ID to shared file so other nodes can discover it
	peerInfoFile := fmt.Sprintf("/tmp/tss-peer-%s.json", *partyID)
	peerInfo := map[string]interface{}{
		"party_id": *partyID,
		"peer_id":  peerID,
		"addrs":    listenAddrs,
	}
	if data, err := json.Marshal(peerInfo); err == nil {
		os.WriteFile(peerInfoFile, data, 0644)
		logger.Debug().Str("file", peerInfoFile).Msg("wrote peer info to file")
	}

	// Setup database file for this node (each node has its own database)
	// In production, these databases are populated by on-chain event listening
	dbPath := fmt.Sprintf("/tmp/tss-%s.db", *partyID)
	database, err := db.OpenFileDB("/tmp", fmt.Sprintf("tss-%s.db", *partyID), true)
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to open database")
	}
	defer database.Close()
	logger.Info().Str("db_path", dbPath).Msg("using node-specific database")

	// Load peer IDs from files written by other nodes, or from command line flag
	peerIDMap := make(map[string]string)

	// First, try to load from files (written by other nodes)
	for i := 1; i <= 3; i++ {
		partyIDKey := fmt.Sprintf("party-%d", i)
		if partyIDKey == *partyID {
			continue // Skip self
		}
		peerInfoFile := fmt.Sprintf("/tmp/tss-peer-%s.json", partyIDKey)
		if data, err := os.ReadFile(peerInfoFile); err == nil {
			var info map[string]interface{}
			if err := json.Unmarshal(data, &info); err == nil {
				if pid, ok := info["peer_id"].(string); ok {
					peerIDMap[partyIDKey] = pid
					logger.Debug().Str("party", partyIDKey).Str("peer_id", pid).Msg("loaded peer ID from file")
				}
			}
		}
	}

	// Override with command line flag if provided
	if *peerIDs != "" {
		pairs := strings.Split(*peerIDs, ",")
		for _, pair := range pairs {
			parts := strings.Split(strings.TrimSpace(pair), ":")
			if len(parts) == 2 {
				peerIDMap[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
			}
		}
	}

	// Create simple data provider for demo
	dataProvider := &staticPushChainDataProvider{
		partyID:   *partyID,
		peerID:    tr.ID(),
		addrs:     listenAddrs,
		peerIDMap: peerIDMap,
		logger:    logger,
	}

	// Get validator address for this party ID
	validatorAddress, err := dataProvider.GetValidatorAddressForParty(*partyID)
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to get validator address for party")
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
		PartyID:           validatorAddress,
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
		PartyID:        validatorAddress,
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

// staticPushChainDataProvider implements PushChainDataProvider for demo/testing.
type staticPushChainDataProvider struct {
	partyID   string
	peerID    string
	addrs     []string
	peerIDMap map[string]string // partyID -> peerID mapping (can be updated)
	logger    zerolog.Logger
}

// refreshPeerIDs reloads peer IDs from files written by other nodes
func (p *staticPushChainDataProvider) refreshPeerIDs() {
	for i := 1; i <= 3; i++ {
		partyIDKey := fmt.Sprintf("party-%d", i)
		if partyIDKey == p.partyID {
			continue // Skip self
		}
		peerInfoFile := fmt.Sprintf("/tmp/tss-peer-%s.json", partyIDKey)
		if data, err := os.ReadFile(peerInfoFile); err == nil {
			var info map[string]interface{}
			if err := json.Unmarshal(data, &info); err == nil {
				if pid, ok := info["peer_id"].(string); ok {
					p.peerIDMap[partyIDKey] = pid
				}
			}
		}
	}
}

// GetLatestBlockNum implements coordinator.PushChainDataProvider.
func (p *staticPushChainDataProvider) GetLatestBlockNum(ctx context.Context) (uint64, error) {
	// Use timestamp as block number for demo
	return uint64(time.Now().Unix()), nil
}

// GetUniversalValidators implements coordinator.PushChainDataProvider.
func (p *staticPushChainDataProvider) GetUniversalValidators(ctx context.Context) ([]*tss.UniversalValidator, error) {
	// Refresh peer IDs from files (other nodes may have started)
	p.refreshPeerIDs()

	// Map validator addresses to party IDs for demo
	validatorToParty := map[string]string{
		"pushvaloper1fv2fm76q7cjnr58wdwyntzrjgtc7qya6n7dmlu": "party-1",
		"pushvaloper12jzrpp4pkucxxvj6hw4dfxsnhcpy6ddty2fl75": "party-2",
		"pushvaloper1vzuw2x3k2ccme70zcgswv8d88kyc07grdpvw3e": "party-3",
	}

	// Helper to get peer ID for a validator address
	getPeerIDForValidator := func(validatorAddr string) string {
		partyID, ok := validatorToParty[validatorAddr]
		if !ok {
			return "unknown"
		}
		// If this is the local party, use the actual peer ID
		if partyID == p.partyID {
			return p.peerID
		}
		// Otherwise, look it up in the peerIDMap
		if peerID, ok := p.peerIDMap[partyID]; ok {
			return peerID
		}
		return "unknown"
	}

	// Return default 3-node setup for demo
	return []*tss.UniversalValidator{
		{
			ValidatorAddress: "pushvaloper1fv2fm76q7cjnr58wdwyntzrjgtc7qya6n7dmlu",
			Status:           tss.UVStatusActive,
			Network: tss.NetworkInfo{
				PeerID:     getPeerIDForValidator("pushvaloper1fv2fm76q7cjnr58wdwyntzrjgtc7qya6n7dmlu"),
				Multiaddrs: []string{"/ip4/127.0.0.1/tcp/39001"},
			},
			JoinedAtBlock: 0,
		},
		{
			ValidatorAddress: "pushvaloper12jzrpp4pkucxxvj6hw4dfxsnhcpy6ddty2fl75",
			Status:           tss.UVStatusActive,
			Network: tss.NetworkInfo{
				PeerID:     getPeerIDForValidator("pushvaloper12jzrpp4pkucxxvj6hw4dfxsnhcpy6ddty2fl75"),
				Multiaddrs: []string{"/ip4/127.0.0.1/tcp/39002"},
			},
			JoinedAtBlock: 0,
		},
		{
			ValidatorAddress: "pushvaloper1vzuw2x3k2ccme70zcgswv8d88kyc07grdpvw3e",
			Status:           tss.UVStatusActive,
			Network: tss.NetworkInfo{
				PeerID:     getPeerIDForValidator("pushvaloper1vzuw2x3k2ccme70zcgswv8d88kyc07grdpvw3e"),
				Multiaddrs: []string{"/ip4/127.0.0.1/tcp/39003"},
			},
			JoinedAtBlock: 0,
		},
	}, nil
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

// GetValidatorAddressForParty returns the validator address for a given party ID.
func (p *staticPushChainDataProvider) GetValidatorAddressForParty(partyID string) (string, error) {
	validatorToParty := map[string]string{
		"pushvaloper1fv2fm76q7cjnr58wdwyntzrjgtc7qya6n7dmlu": "party-1",
		"pushvaloper12jzrpp4pkucxxvj6hw4dfxsnhcpy6ddty2fl75": "party-2",
		"pushvaloper1vzuw2x3k2ccme70zcgswv8d88kyc07grdpvw3e": "party-3",
	}

	// Reverse lookup: party ID -> validator address
	for validatorAddr, pID := range validatorToParty {
		if pID == partyID {
			return validatorAddr, nil
		}
	}
	return "", fmt.Errorf("no validator address found for party: %s", partyID)
}
