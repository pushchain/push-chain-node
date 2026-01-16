package chains

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	pkgerrors "github.com/pkg/errors"
	"github.com/pushchain/push-chain-node/universalClient/chains/common"
	"github.com/pushchain/push-chain-node/universalClient/chains/evm"
	"github.com/pushchain/push-chain-node/universalClient/chains/push"
	"github.com/pushchain/push-chain-node/universalClient/chains/svm"
	"github.com/pushchain/push-chain-node/universalClient/config"
	"github.com/pushchain/push-chain-node/universalClient/constant"
	"github.com/pushchain/push-chain-node/universalClient/db"
	"github.com/pushchain/push-chain-node/universalClient/pushcore"
	"github.com/pushchain/push-chain-node/universalClient/pushsigner"
	uregistrytypes "github.com/pushchain/push-chain-node/x/uregistry/types"
	"github.com/rs/zerolog"
)

// Chains manages chain clients by fetching chain configs periodically and adding/removing clients accordingly
type Chains struct {
	pushCore   *pushcore.Client
	pushSigner *pushsigner.Signer
	config     *config.Config
	logger     zerolog.Logger

	// Chain client management
	chains       map[string]common.ChainClient          // key: CAIP-2 chain ID
	chainConfigs map[string]*uregistrytypes.ChainConfig // key: CAIP-2 chain ID
	chainsMu     sync.RWMutex
	pushChainID  string // Push chain ID (always present)

	// Background control
	muRunning sync.Mutex
	running   bool
	stopCh    chan struct{}
	wg        sync.WaitGroup
}

const (
	// perSyncTimeout is the timeout for each sync operation
	perSyncTimeout = 30 * time.Second
)

// NewChains creates a new chains manager
func NewChains(
	pushCore *pushcore.Client,
	pushSigner *pushsigner.Signer,
	cfg *config.Config,
	logger zerolog.Logger,
) *Chains {
	return &Chains{
		pushCore:     pushCore,
		pushSigner:   pushSigner,
		config:       cfg,
		logger:       logger.With().Str("component", "chains").Logger(),
		chains:       make(map[string]common.ChainClient),
		chainConfigs: make(map[string]*uregistrytypes.ChainConfig),
		pushChainID:  cfg.PushChainID,
	}
}

// Start begins fetching chains and managing chain clients
func (c *Chains) Start(ctx context.Context) error {
	c.muRunning.Lock()
	defer c.muRunning.Unlock()

	if c.running {
		return nil
	}

	if c.pushCore == nil {
		return fmt.Errorf("pushCore must be non-nil")
	}

	c.running = true
	c.stopCh = make(chan struct{})
	c.wg.Add(1)

	// Always create push chain client first
	if err := c.ensurePushChain(ctx); err != nil {
		c.logger.Warn().Err(err).Msg("failed to create push chain client; continuing")
	}

	go c.run(ctx)
	return nil
}

// Stop stops the chains manager
func (c *Chains) Stop() {
	c.muRunning.Lock()
	if !c.running {
		c.muRunning.Unlock()
		return
	}
	close(c.stopCh)
	c.running = false
	c.muRunning.Unlock()

	c.wg.Wait()

	// Stop all chain clients
	c.StopAll()
}

// run executes the main loop
func (c *Chains) run(parent context.Context) {
	defer c.wg.Done()

	// Initial fetch
	if err := c.fetchAndUpdate(parent); err != nil {
		c.logger.Warn().Err(err).Msg("initial chain fetch failed; continuing")
	}

	// Periodic updates - get interval from config
	interval := time.Duration(c.config.ConfigRefreshIntervalSeconds) * time.Second
	if interval <= 0 {
		interval = 60 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-parent.Done():
			c.logger.Info().Msg("chains: context canceled; stopping")
			return
		case <-c.stopCh:
			c.logger.Info().Msg("chains: stop requested; stopping")
			return
		case <-ticker.C:
			if err := c.fetchAndUpdate(parent); err != nil {
				c.logger.Warn().Err(err).Msg("periodic chain fetch failed; keeping previous chains")
			}
		}
	}
}

// fetchAndUpdate fetches chain configs and updates chain clients
func (c *Chains) fetchAndUpdate(parent context.Context) error {
	timeout := perSyncTimeout
	if dl, ok := parent.Deadline(); ok {
		if remain := time.Until(dl); remain > 0 && remain < timeout {
			timeout = remain
		}
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	// Fetch chain configs from pushcore (does NOT return Push chain)
	cfgs, err := c.pushCore.GetAllChainConfigs(ctx)
	if err != nil {
		return err
	}

	// Track seen chains (Push chain always marked as seen)
	seenChains := make(map[string]bool)
	if c.pushChainID != "" {
		seenChains[c.pushChainID] = true
	}

	// Process each chain config
	for _, cfg := range cfgs {
		chainID := cfg.Chain
		if chainID == "" || chainID == c.pushChainID {
			continue
		}

		seenChains[chainID] = true
		action := c.determineChainAction(cfg)

		switch action {
		case chainActionSkip:
			// Disabled or no change - skip
			continue

		case chainActionAdd:
			if err := c.addChain(parent, cfg); err != nil {
				c.logger.Error().Err(err).Str("chain", chainID).Msg("failed to add chain")
			}

		case chainActionUpdate:
			c.logger.Info().Str("chain", chainID).Msg("chain config changed, updating")
			if err := c.removeChain(chainID); err != nil {
				c.logger.Error().Err(err).Str("chain", chainID).Msg("failed to remove chain for update")
			}
			if err := c.addChain(parent, cfg); err != nil {
				c.logger.Error().Err(err).Str("chain", chainID).Msg("failed to add updated chain")
			}
		}
	}

	// Remove stale chains (never remove Push chain)
	c.chainsMu.RLock()
	for chainID := range c.chains {
		if chainID != c.pushChainID && !seenChains[chainID] {
			c.logger.Info().Str("chain", chainID).Msg("removing chain no longer in config")
			if err := c.removeChain(chainID); err != nil {
				c.logger.Error().Err(err).Str("chain", chainID).Msg("failed to remove chain")
			}
		}
	}
	c.chainsMu.RUnlock()

	// Ensure Push chain is always present
	if err := c.ensurePushChain(parent); err != nil {
		c.logger.Warn().Err(err).Msg("failed to ensure push chain client")
	}

	return nil
}

// chainAction represents the action to take for a chain config
type chainAction int

const (
	chainActionSkip chainAction = iota
	chainActionAdd
	chainActionUpdate
	chainActionRemove
)

// determineChainAction determines what action to take for a chain config
func (c *Chains) determineChainAction(cfg *uregistrytypes.ChainConfig) chainAction {
	chainID := cfg.Chain

	// Skip disabled chains
	if cfg.Enabled == nil || (!cfg.Enabled.IsInboundEnabled && !cfg.Enabled.IsOutboundEnabled) {
		c.logger.Debug().Str("chain", chainID).Msg("chain is disabled, skipping")
		return chainActionSkip
	}

	// Check if chain exists
	c.chainsMu.RLock()
	_, exists := c.chains[chainID]
	existingConfig := c.chainConfigs[chainID]
	c.chainsMu.RUnlock()

	if !exists {
		return chainActionAdd
	}

	// Check if config changed
	if existingConfig != nil && !configsEqual(existingConfig, cfg) {
		return chainActionUpdate
	}

	// No change
	return chainActionSkip
}

// addChain adds a new chain client
func (c *Chains) addChain(ctx context.Context, cfg *uregistrytypes.ChainConfig) error {
	if cfg == nil || cfg.Chain == "" {
		return fmt.Errorf("invalid chain config")
	}

	// Get or create database for this chain
	chainDB, err := c.getChainDB(cfg.Chain)
	if err != nil {
		return fmt.Errorf("failed to get database for chain %s: %w", cfg.Chain, err)
	}

	// Get chain-specific config
	chainConfig := c.config.GetChainConfig(cfg.Chain)

	// Create chain client based on VM type
	var client common.ChainClient
	switch cfg.VmType {
	case uregistrytypes.VmType_EVM:
		client, err = evm.NewClient(cfg, chainDB, chainConfig, c.pushSigner, c.logger)
	case uregistrytypes.VmType_SVM:
		client, err = svm.NewClient(cfg, chainDB, chainConfig, c.pushSigner, c.logger)
	default:
		return fmt.Errorf("unsupported VM type: %v", cfg.VmType)
	}

	if err != nil {
		return fmt.Errorf("failed to create chain client: %w", err)
	}

	// Start the chain client
	if err := client.Start(ctx); err != nil {
		return fmt.Errorf("failed to start chain client: %w", err)
	}

	// Store the client and config
	c.chainsMu.Lock()
	c.chains[cfg.Chain] = client
	c.chainConfigs[cfg.Chain] = cfg
	c.chainsMu.Unlock()

	c.logger.Info().
		Str("chain", cfg.Chain).
		Msg("successfully added chain client")

	return nil
}

// removeChain removes a chain client
func (c *Chains) removeChain(chainID string) error {
	c.chainsMu.Lock()
	defer c.chainsMu.Unlock()

	client, exists := c.chains[chainID]
	if !exists {
		return nil
	}

	c.logger.Info().
		Str("chain", chainID).
		Msg("removing chain client")

	// Stop the client
	if err := client.Stop(); err != nil {
		c.logger.Error().
			Err(err).
			Str("chain", chainID).
			Msg("error stopping chain client during removal")
	}

	delete(c.chains, chainID)
	delete(c.chainConfigs, chainID)
	return nil
}

// StopAll stops all chain clients
func (c *Chains) StopAll() {
	c.chainsMu.Lock()
	defer c.chainsMu.Unlock()

	c.logger.Info().Msg("stopping all chain clients")

	for chainID, client := range c.chains {
		if err := client.Stop(); err != nil {
			c.logger.Error().
				Err(err).
				Str("chain", chainID).
				Msg("error stopping chain client")
		}
	}

	// Clear the registry
	c.chains = make(map[string]common.ChainClient)
	c.chainConfigs = make(map[string]*uregistrytypes.ChainConfig)
}

// GetClient returns the chain client for the specified chain ID
func (c *Chains) GetClient(chainID string) (common.ChainClient, error) {
	c.chainsMu.RLock()
	defer c.chainsMu.RUnlock()

	client, exists := c.chains[chainID]
	if !exists {
		return nil, fmt.Errorf("chain client not found for chain %s", chainID)
	}

	return client, nil
}

// getChainDB returns a database instance for a specific chain
func (c *Chains) getChainDB(chainID string) (*db.DB, error) {
	// Create database file directly named after the chain's CAIP-2 format
	// e.g., "eip155:1" -> "eip155_1.db"
	sanitizedChainID := sanitizeChainID(chainID)
	dbFilename := sanitizedChainID + ".db"

	// Derive database base directory from NodeHome
	baseDir := filepath.Join(c.config.NodeHome, constant.DatabasesSubdir)

	database, err := db.OpenFileDB(baseDir, dbFilename, true)
	if err != nil {
		return nil, pkgerrors.Wrapf(err, "failed to create database for chain %s", chainID)
	}

	c.logger.Info().
		Str("chain_id", chainID).
		Str("db_path", filepath.Join(baseDir, dbFilename)).
		Msg("created file database for chain")

	return database, nil
}

// ensurePushChain ensures the push chain client is always present
func (c *Chains) ensurePushChain(ctx context.Context) error {
	if c.pushChainID == "" {
		return fmt.Errorf("push chain ID not configured")
	}

	c.chainsMu.RLock()
	_, exists := c.chains[c.pushChainID]
	c.chainsMu.RUnlock()

	if exists {
		return nil // Already exists
	}

	// Get or create database for push chain
	pushDB, err := c.getChainDB(c.pushChainID)
	if err != nil {
		return fmt.Errorf("failed to get database for push chain: %w", err)
	}

	// Create a minimal chain config for push chain
	// Push chain doesn't need gateway or other configs
	pushConfig := &uregistrytypes.ChainConfig{
		Chain: c.pushChainID,
		// VmType is not set for push chain as it's not a gateway chain
		// GatewayAddress is empty for push chain
		Enabled: &uregistrytypes.ChainEnabled{
			IsInboundEnabled:  true,
			IsOutboundEnabled: true,
		},
	}

	// Get chain-specific config for push chain
	chainConfig := c.config.GetChainConfig(c.pushChainID)

	// Create push chain client
	client, err := push.NewClient(
		pushDB,
		chainConfig,
		c.pushCore,
		c.pushChainID,
		c.logger,
	)
	if err != nil {
		return fmt.Errorf("failed to create push chain client: %w", err)
	}

	// Start the push chain client
	if err := client.Start(ctx); err != nil {
		return fmt.Errorf("failed to start push chain client: %w", err)
	}

	// Store the client and config
	c.chainsMu.Lock()
	c.chains[c.pushChainID] = client
	c.chainConfigs[c.pushChainID] = pushConfig
	c.chainsMu.Unlock()

	c.logger.Info().
		Str("chain", c.pushChainID).
		Msg("successfully added push chain client")

	return nil
}

// Helper functions

// sanitizeChainID converts chain ID to filesystem-safe format
// e.g., "eip155:1" -> "eip155_1"
func sanitizeChainID(chainID string) string {
	result := ""
	for _, r := range chainID {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			result += string(r)
		} else {
			result += "_"
		}
	}
	return result
}

// configsEqual compares two chain configurations
func configsEqual(a, b *uregistrytypes.ChainConfig) bool {
	if a == nil || b == nil {
		return a == b
	}

	// Handle Enabled field comparison
	enabledEqual := false
	if a.Enabled == nil && b.Enabled == nil {
		enabledEqual = true
	} else if a.Enabled != nil && b.Enabled != nil {
		enabledEqual = a.Enabled.IsInboundEnabled == b.Enabled.IsInboundEnabled &&
			a.Enabled.IsOutboundEnabled == b.Enabled.IsOutboundEnabled
	}

	// Compare relevant fields
	return a.Chain == b.Chain &&
		a.VmType == b.VmType &&
		a.GatewayAddress == b.GatewayAddress &&
		enabledEqual
}
