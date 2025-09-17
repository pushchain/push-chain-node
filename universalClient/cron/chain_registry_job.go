// cron/chain_cache_job.go
package cron

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/pushchain/push-chain-node/universalClient/cache"
	"github.com/pushchain/push-chain-node/universalClient/chains"
)

type ChainRegistryJob struct {
	cache          *cache.Cache
	chainRegistry  *chains.ChainRegistry
	interval       time.Duration
	perSyncTimeout time.Duration
	logger         zerolog.Logger

	mu      sync.Mutex
	running bool
	stopCh  chan struct{}
	forceCh chan struct{}
	wg      sync.WaitGroup
}

func NewChainRegistryJob(ca *cache.Cache, cr *chains.ChainRegistry, interval, perSyncTimeout time.Duration, logger zerolog.Logger) *ChainRegistryJob {
	if interval <= 0 {
		interval = time.Minute
	}
	if perSyncTimeout <= 0 {
		perSyncTimeout = 8 * time.Second
	}
	return &ChainRegistryJob{
		cache:          ca,
		chainRegistry:  cr,
		interval:       interval,
		perSyncTimeout: perSyncTimeout,
		logger:         logger.With().Str("component", "chain_registry_cron").Logger(),
	}
}

// Start launches the background loop and returns immediately (non-blocking).
// Safe to call multiple times; subsequent calls are no-ops.
func (j *ChainRegistryJob) Start(ctx context.Context) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.running {
		return nil
	}
	if j.cache == nil || j.chainRegistry == nil {
		return errors.New("cron: cache and chainRegistry must be non-nil")
	}

	j.stopCh = make(chan struct{})
	j.forceCh = make(chan struct{}, 1) // buffered so ForceSync won't block
	j.running = true
	j.wg.Add(1)

	go j.run(ctx)
	return nil
}

// Stop signals the loop to exit and waits for it to finish.
// Safe to call multiple times.
func (j *ChainRegistryJob) Stop() {
	j.mu.Lock()
	if !j.running {
		j.mu.Unlock()
		return
	}
	close(j.stopCh)
	j.running = false
	j.mu.Unlock()
	j.wg.Wait()
}

func (j *ChainRegistryJob) run(parent context.Context) {
	defer j.wg.Done()

	t := time.NewTicker(j.interval)
	defer t.Stop()

	for {
		select {
		case <-parent.Done():
			j.logger.Info().Msg("chain cache cron: context canceled; stopping")
			return
		case <-j.stopCh:
			j.logger.Info().Msg("chain cache cron: stop requested; stopping")
			return
		case <-t.C:
			if err := j.syncOnce(parent); err != nil {
				j.logger.Warn().Err(err).Msg("periodic chain config refresh failed; keeping previous cache")
			}
		case <-j.forceCh:
			if err := j.syncOnce(parent); err != nil {
				j.logger.Warn().Err(err).Msg("forced chain config refresh failed; keeping previous cache")
			}
		}
	}
}

func (j *ChainRegistryJob) syncOnce(parent context.Context) error {
	timeout := j.perSyncTimeout
	if dl, ok := parent.Deadline(); ok {
		if remain := time.Until(dl); remain > 0 && remain < timeout {
			timeout = remain
		}
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	chainConfigs := j.cache.GetAllChains()

	// Track which chains we've seen
	seenChains := make(map[string]bool)

	for _, chain := range chainConfigs {
		config := chain.Config
		if config == nil || config.Chain == "" {
			continue
		}

		seenChains[config.Chain] = true

		if config.Enabled == nil || (!config.Enabled.IsInboundEnabled && !config.Enabled.IsOutboundEnabled) {
			j.logger.Debug().
				Str("chain", config.Chain).
				Msg("chain is disabled, removing if exists")
			j.chainRegistry.RemoveChain(config.Chain)
			continue
		}

		// Add or update the chain
		if err := j.chainRegistry.AddOrUpdateChain(ctx, config); err != nil {
			j.logger.Error().
				Err(err).
				Str("chain", config.Chain).
				Msg("failed to add/update chain")
			// Continue with other chains
		}
	}

	// Remove chains that no longer exist in the config
	allChains := j.chainRegistry.GetAllChains()
	for chainID := range allChains {
		if !seenChains[chainID] {
			j.logger.Info().
				Str("chain", chainID).
				Msg("removing chain no longer in config")
			j.chainRegistry.RemoveChain(chainID)
		}
	}

	return nil
}
