package core

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/pushchain/push-chain-node/universalClient/chains"
	"github.com/pushchain/push-chain-node/universalClient/chains/common"
	"github.com/pushchain/push-chain-node/universalClient/config"
)

// GasPriceFetcher handles periodic gas price fetching for all chains
type GasPriceFetcher struct {
	chainRegistry *chains.ChainRegistry
	config        *config.Config
	logger        zerolog.Logger
	
	mu             sync.RWMutex
	chainFetchers  map[string]*chainGasPriceFetcher // Per-chain fetchers
	stopCh         chan struct{}
	stopOnce       sync.Once
}

// chainGasPriceFetcher handles gas price fetching for a single chain
type chainGasPriceFetcher struct {
	chainID  string
	client   common.ChainClient
	ticker   *time.Ticker
	interval time.Duration
	stopCh   chan struct{}
	logger   zerolog.Logger
}

// NewGasPriceFetcher creates a new gas price fetcher
func NewGasPriceFetcher(
	chainRegistry *chains.ChainRegistry,
	cfg *config.Config,
	logger zerolog.Logger,
) *GasPriceFetcher {
	return &GasPriceFetcher{
		chainRegistry: chainRegistry,
		config:        cfg,
		logger:        logger.With().Str("component", "gas_price_fetcher").Logger(),
		chainFetchers: make(map[string]*chainGasPriceFetcher),
		stopCh:        make(chan struct{}),
	}
}

// Start begins the gas price fetching process for all chains
func (f *GasPriceFetcher) Start(ctx context.Context) error {
	f.logger.Info().Msg("starting gas price fetcher")

	// Get all registered chains
	chains := f.chainRegistry.GetAllChains()
	
	f.mu.Lock()
	defer f.mu.Unlock()

	// Start a fetcher for each chain
	for chainID, client := range chains {
		if client == nil || !client.IsHealthy() {
			f.logger.Warn().
				Str("chain", chainID).
				Msg("skipping unhealthy or nil chain")
			continue
		}

		// Get the interval for this chain
		intervalSeconds := f.config.GetChainGasPriceInterval(chainID)
		interval := time.Duration(intervalSeconds) * time.Second

		f.logger.Info().
			Str("chain", chainID).
			Str("interval", interval.String()).
			Msg("starting gas price fetcher for chain")

		// Create and start the chain fetcher
		fetcher := &chainGasPriceFetcher{
			chainID:  chainID,
			client:   client,
			interval: interval,
			stopCh:   make(chan struct{}),
			logger: f.logger.With().
				Str("chain", chainID).
				Logger(),
		}

		// Perform initial fetch
		f.fetchGasPrice(ctx, fetcher)

		// Start periodic fetching
		fetcher.ticker = time.NewTicker(interval)
		go f.runChainFetcher(ctx, fetcher)

		f.chainFetchers[chainID] = fetcher
	}

	// Watch for chain updates
	go f.watchChainUpdates(ctx)

	return nil
}

// Stop halts all gas price fetchers
func (f *GasPriceFetcher) Stop() {
	f.stopOnce.Do(func() {
		f.logger.Info().Msg("stopping gas price fetcher")

		close(f.stopCh)

		f.mu.Lock()
		defer f.mu.Unlock()

		// Stop all chain fetchers
		for chainID, fetcher := range f.chainFetchers {
			f.logger.Debug().
				Str("chain", chainID).
				Msg("stopping chain gas price fetcher")
			
			if fetcher.ticker != nil {
				fetcher.ticker.Stop()
			}
			close(fetcher.stopCh)
		}

		// Clear the map
		f.chainFetchers = make(map[string]*chainGasPriceFetcher)
	})
}

// runChainFetcher runs the periodic gas price fetching for a single chain
func (f *GasPriceFetcher) runChainFetcher(ctx context.Context, fetcher *chainGasPriceFetcher) {
	for {
		select {
		case <-ctx.Done():
			fetcher.logger.Debug().Msg("context cancelled, stopping fetcher")
			return
		case <-fetcher.stopCh:
			fetcher.logger.Debug().Msg("stop signal received, stopping fetcher")
			return
		case <-fetcher.ticker.C:
			f.fetchGasPrice(ctx, fetcher)
		}
	}
}

// fetchGasPrice fetches the gas price for a single chain
func (f *GasPriceFetcher) fetchGasPrice(ctx context.Context, fetcher *chainGasPriceFetcher) {
	// Create a timeout context for the fetch operation
	fetchCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	startTime := time.Now()
	gasPrice, err := fetcher.client.GetGasPrice(fetchCtx)
	duration := time.Since(startTime)

	if err != nil {
		fetcher.logger.Error().
			Err(err).
			Dur("duration", duration).
			Msg("failed to fetch gas price")
		return
	}

	// Log the gas price with structured data
	fetcher.logger.Info().
		Str("gas_price", gasPrice.String()).
		Dur("fetch_duration", duration).
		Int64("timestamp", time.Now().Unix()).
		Msg("gas price fetched successfully")

	// TODO: In the future, store this in a database or metrics system
	// For now, just logging is sufficient as requested
}

// watchChainUpdates watches for chain additions/removals and updates fetchers accordingly
func (f *GasPriceFetcher) watchChainUpdates(ctx context.Context) {
	// Check for updates every 30 seconds
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-f.stopCh:
			return
		case <-ticker.C:
			f.updateChainFetchers(ctx)
		}
	}
}

// updateChainFetchers updates the list of active chain fetchers based on current chains
func (f *GasPriceFetcher) updateChainFetchers(ctx context.Context) {
	currentChains := f.chainRegistry.GetAllChains()
	
	f.mu.Lock()
	defer f.mu.Unlock()

	// Check for new chains
	for chainID, client := range currentChains {
		if _, exists := f.chainFetchers[chainID]; !exists {
			// New chain detected
			if client == nil || !client.IsHealthy() {
				continue
			}

			intervalSeconds := f.config.GetChainGasPriceInterval(chainID)
			interval := time.Duration(intervalSeconds) * time.Second

			f.logger.Info().
				Str("chain", chainID).
				Str("interval", interval.String()).
				Msg("adding gas price fetcher for new chain")

			fetcher := &chainGasPriceFetcher{
				chainID:  chainID,
				client:   client,
				interval: interval,
				stopCh:   make(chan struct{}),
				logger: f.logger.With().
					Str("chain", chainID).
					Logger(),
			}

			// Perform initial fetch
			f.fetchGasPrice(ctx, fetcher)

			// Start periodic fetching
			fetcher.ticker = time.NewTicker(interval)
			go f.runChainFetcher(ctx, fetcher)

			f.chainFetchers[chainID] = fetcher
		}
	}

	// Check for removed chains
	for chainID, fetcher := range f.chainFetchers {
		if _, exists := currentChains[chainID]; !exists {
			// Chain removed
			f.logger.Info().
				Str("chain", chainID).
				Msg("removing gas price fetcher for removed chain")

			if fetcher.ticker != nil {
				fetcher.ticker.Stop()
			}
			close(fetcher.stopCh)
			delete(f.chainFetchers, chainID)
		}
	}
}

// UpdateChainInterval updates the fetching interval for a specific chain
func (f *GasPriceFetcher) UpdateChainInterval(chainID string, intervalSeconds int) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	fetcher, exists := f.chainFetchers[chainID]
	if !exists {
		return fmt.Errorf("no fetcher found for chain %s", chainID)
	}

	newInterval := time.Duration(intervalSeconds) * time.Second
	if fetcher.interval == newInterval {
		// No change needed
		return nil
	}

	f.logger.Info().
		Str("chain", chainID).
		Str("old_interval", fetcher.interval.String()).
		Str("new_interval", newInterval.String()).
		Msg("updating gas price fetch interval")

	// Stop the old ticker
	if fetcher.ticker != nil {
		fetcher.ticker.Stop()
	}

	// Update interval and create new ticker
	fetcher.interval = newInterval
	fetcher.ticker = time.NewTicker(newInterval)

	return nil
}

// GetActiveFetchers returns the list of active chain IDs being fetched
func (f *GasPriceFetcher) GetActiveFetchers() []string {
	f.mu.RLock()
	defer f.mu.RUnlock()

	chains := make([]string, 0, len(f.chainFetchers))
	for chainID := range f.chainFetchers {
		chains = append(chains, chainID)
	}
	return chains
}