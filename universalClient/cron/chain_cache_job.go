// cron/chain_cache_job.go
package cron

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/pushchain/push-chain-node/universalClient/cache"
	"github.com/pushchain/push-chain-node/universalClient/pushcore"
)

type ChainCacheJob struct {
	cache          *cache.Cache
	client         *pushcore.Client
	interval       time.Duration
	perSyncTimeout time.Duration
	logger         zerolog.Logger

	mu      sync.Mutex
	running bool
	stopCh  chan struct{}
	forceCh chan struct{}
	wg      sync.WaitGroup
}

func NewChainCacheJob(ca *cache.Cache, cl *pushcore.Client, interval, perSyncTimeout time.Duration, logger zerolog.Logger) *ChainCacheJob {
	if interval <= 0 {
		interval = time.Minute
	}
	if perSyncTimeout <= 0 {
		perSyncTimeout = 8 * time.Second
	}
	return &ChainCacheJob{
		cache:          ca,
		client:         cl,
		interval:       interval,
		perSyncTimeout: perSyncTimeout,
		logger:         logger.With().Str("component", "chain_cache_cron").Logger(),
	}
}

// Start launches the background loop and returns immediately (non-blocking).
// Safe to call multiple times; subsequent calls are no-ops.
func (j *ChainCacheJob) Start(ctx context.Context) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.running {
		return nil
	}
	if j.cache == nil || j.client == nil {
		return errors.New("cron: cache and client must be non-nil")
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
func (j *ChainCacheJob) Stop() {
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

func (j *ChainCacheJob) run(parent context.Context) {
	defer j.wg.Done()

	// Initial sync with 3 retries (1s, 2s, 4s)
	if err := j.initialSync(parent); err != nil {
		j.logger.Warn().Err(err).Msg("initial chain config sync failed; continuing with empty/stale cache")
	}

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

func (j *ChainCacheJob) initialSync(ctx context.Context) error {
	backoff := time.Second
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		if err := j.syncOnce(ctx); err != nil {
			lastErr = err
			j.logger.Warn().Int("attempt", attempt).Err(err).Msg("initial chain config sync attempt failed")
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
				backoff *= 2
			}
			continue
		}
		j.logger.Info().Int("attempt", attempt).Msg("initial chain config sync successful")
		return nil
	}
	return lastErr
}

func (j *ChainCacheJob) syncOnce(parent context.Context) error {
	timeout := j.perSyncTimeout
	if dl, ok := parent.Deadline(); ok {
		if remain := time.Until(dl); remain > 0 && remain < timeout {
			timeout = remain
		}
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	cfgs, err := j.client.GetAllChainConfigs(ctx)
	if err != nil {
		return err
	}
	if len(cfgs) == 0 {
		return errors.New("fetched zero chain configs")
	}

	j.cache.UpdateChains(cfgs)
	return nil
}
