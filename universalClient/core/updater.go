package core

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog"

	"github.com/pushchain/push-chain-node/universalClient/chains"
	"github.com/pushchain/push-chain-node/universalClient/config"
	"github.com/pushchain/push-chain-node/universalClient/registry"
	uregistrytypes "github.com/pushchain/push-chain-node/x/uregistry/types"
)

// ConfigUpdater handles periodic updates of chain and token configurations
type ConfigUpdater struct {
	registry     RegistryInterface
	cache        *registry.ConfigCache
	chainReg     *chains.ChainRegistry
	config       *config.Config
	ticker       *time.Ticker
	logger       zerolog.Logger
	stopCh       chan struct{}
	updatePeriod time.Duration
}

// NewConfigUpdater creates a new configuration updater
func NewConfigUpdater(
	registryClient RegistryInterface,
	cache *registry.ConfigCache,
	chainRegistry *chains.ChainRegistry,
	cfg *config.Config,
	logger zerolog.Logger,
) *ConfigUpdater {
	return &ConfigUpdater{
		registry:     registryClient,
		cache:        cache,
		chainReg:     chainRegistry,
		config:       cfg,
		updatePeriod: cfg.ConfigRefreshInterval,
		logger:       logger.With().Str("component", "config_updater").Logger(),
		stopCh:       make(chan struct{}),
	}
}

// Start begins the periodic update process
func (u *ConfigUpdater) Start(ctx context.Context) error {
	u.logger.Info().
		Dur("update_period", u.updatePeriod).
		Msg("starting config updater")

	// Perform initial update with retries
	err := u.performInitialUpdate(ctx)
	if err != nil {
		return fmt.Errorf("failed to perform initial configuration update: %w", err)
	}

	// Start periodic updates
	u.ticker = time.NewTicker(u.updatePeriod)
	go u.runUpdateLoop(ctx)

	return nil
}

// performInitialUpdate attempts to fetch initial configuration with retries
func (u *ConfigUpdater) performInitialUpdate(ctx context.Context) error {
	u.logger.Info().
		Int("max_retries", u.config.InitialFetchRetries).
		Dur("timeout", u.config.InitialFetchTimeout).
		Msg("performing initial configuration fetch")

	backoff := u.config.RetryBackoff

	for attempt := 1; attempt <= u.config.InitialFetchRetries; attempt++ {
		// Create timeout context for this attempt
		attemptCtx, cancel := context.WithTimeout(ctx, u.config.InitialFetchTimeout)

		u.logger.Info().
			Int("attempt", attempt).
			Int("max_attempts", u.config.InitialFetchRetries).
			Msg("attempting to fetch initial configuration")

		err := u.updateConfigs(attemptCtx)
		cancel()

		if err == nil {
			u.logger.Info().
				Int("attempt", attempt).
				Msg("successfully fetched initial configuration")
			return nil
		}

		u.logger.Error().
			Err(err).
			Int("attempt", attempt).
			Int("max_attempts", u.config.InitialFetchRetries).
			Dur("next_retry_in", backoff).
			Msg("initial config fetch failed")

		// Check if this was the last attempt
		if attempt == u.config.InitialFetchRetries {
			return fmt.Errorf("failed to fetch initial configuration after %d attempts: %w", attempt, err)
		}

		// Wait before next retry with exponential backoff
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
			// Exponential backoff with cap at 30 seconds
			backoff = backoff * 2
			if backoff > 30*time.Second {
				backoff = 30 * time.Second
			}
		}
	}

	return fmt.Errorf("failed to fetch initial configuration")
}

// Stop halts the periodic update process
func (u *ConfigUpdater) Stop() {
	u.logger.Info().Msg("stopping config updater")

	if u.ticker != nil {
		u.ticker.Stop()
	}
	close(u.stopCh)
}

// runUpdateLoop runs the periodic update loop
func (u *ConfigUpdater) runUpdateLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			u.logger.Info().Msg("context cancelled, stopping update loop")
			return
		case <-u.stopCh:
			u.logger.Info().Msg("stop signal received, stopping update loop")
			return
		case <-u.ticker.C:
			u.logger.Debug().Msg("update ticker fired")
			if err := u.updateConfigs(ctx); err != nil {
				u.logger.Error().
					Err(err).
					Msg("periodic config update failed")
			}
		}
	}
}

// updateConfigs fetches the latest configurations and updates the cache
func (u *ConfigUpdater) updateConfigs(ctx context.Context) error {
	u.logger.Info().Msg("updating configurations from registry")

	// Create a timeout context for the update operation
	updateCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// Fetch all chain configs
	chainConfigs, err := u.registry.GetAllChainConfigs(updateCtx)
	if err != nil {
		return fmt.Errorf("failed to fetch chain configs: %w", err)
	}

	u.logger.Debug().
		Int("chain_count", len(chainConfigs)).
		Msg("fetched chain configs")

	// Fetch all token configs
	tokenConfigs, err := u.registry.GetAllTokenConfigs(updateCtx)
	if err != nil {
		return fmt.Errorf("failed to fetch token configs: %w", err)
	}

	u.logger.Debug().
		Int("token_count", len(tokenConfigs)).
		Msg("fetched token configs")

	// Update cache atomically
	u.cache.UpdateAll(chainConfigs, tokenConfigs)

	// Update chain clients based on VM type
	if err := u.updateChainClients(ctx, chainConfigs); err != nil {
		// Log but don't fail - we still have updated configs in cache
		u.logger.Error().
			Err(err).
			Msg("failed to update some chain clients")
	}

	u.logger.Info().
		Int("chains", len(chainConfigs)).
		Int("tokens", len(tokenConfigs)).
		Msg("successfully updated configurations")

	return nil
}

// updateChainClients updates the chain registry with new/modified chains
func (u *ConfigUpdater) updateChainClients(ctx context.Context, chainConfigs []*uregistrytypes.ChainConfig) error {
	// Track which chains we've seen
	seenChains := make(map[string]bool)

	for _, config := range chainConfigs {
		if config == nil || config.Chain == "" {
			continue
		}

		seenChains[config.Chain] = true

		// TODO: check it once @shoaib
		if !config.Enabled.IsInboundEnabled {
			u.logger.Debug().
				Str("chain", config.Chain).
				Msg("chain is disabled, removing if exists")
			u.chainReg.RemoveChain(config.Chain)
			continue
		}

		// Add or update the chain
		if err := u.chainReg.AddOrUpdateChain(ctx, config); err != nil {
			u.logger.Error().
				Err(err).
				Str("chain", config.Chain).
				Msg("failed to add/update chain")
			// Continue with other chains
		}
	}

	// Remove chains that no longer exist in the config
	allChains := u.chainReg.GetAllChains()
	for chainID := range allChains {
		if !seenChains[chainID] {
			u.logger.Info().
				Str("chain", chainID).
				Msg("removing chain no longer in config")
			u.chainReg.RemoveChain(chainID)
		}
	}

	return nil
}

// ForceUpdate triggers an immediate configuration update
func (u *ConfigUpdater) ForceUpdate(ctx context.Context) error {
	u.logger.Info().Msg("forcing configuration update")
	return u.updateConfigs(ctx)
}
