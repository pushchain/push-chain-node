package cache

import (
	"sync"
	"time"

	uregistrytypes "github.com/pushchain/push-chain-node/x/uregistry/types"
	"github.com/rs/zerolog"
)

// ChainData holds a chain config and when it was last updated.
type ChainData struct {
	Config    *uregistrytypes.ChainConfig
	UpdatedAt time.Time
}

// Cache is a thread-safe store for chain configs.
// Data can only be changed via UpdateChains.
type Cache struct {
	mu         sync.RWMutex
	chains     map[string]*ChainData
	lastUpdate time.Time
	logger     zerolog.Logger
}

// New creates a new Cache instance.
func New(logger zerolog.Logger) *Cache {
	return &Cache{
		chains: make(map[string]*ChainData),
		logger: logger.With().Str("component", "cache").Logger(),
	}
}

// LastUpdated returns the last time the cache was refreshed.
func (c *Cache) LastUpdated() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lastUpdate
}

// UpdateChains atomically replaces the entire cache.
func (c *Cache) UpdateChains(chains []*uregistrytypes.ChainConfig) {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	newMap := make(map[string]*ChainData, len(chains))

	for _, cfg := range chains {
		if cfg == nil || cfg.Chain == "" {
			continue
		}
		newMap[cfg.Chain] = &ChainData{
			Config:    cfg,
			UpdatedAt: now,
		}
	}

	c.chains = newMap
	c.lastUpdate = now

	c.logger.Info().
		Int("chains", len(newMap)).
		Time("updated_at", now).
		Msg("cache updated")
}

// GetChainData returns a pointer copy of a chain's data, safe for reading.
// If not found, returns nil.
func (c *Cache) GetChainData(chainID string) *ChainData {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if data, ok := c.chains[chainID]; ok {
		return &ChainData{
			Config:    data.Config,
			UpdatedAt: data.UpdatedAt,
		}
	}
	return nil
}

// GetAllChains returns a slice copy of all chain data.
func (c *Cache) GetAllChains() []*ChainData {
	c.mu.RLock()
	defer c.mu.RUnlock()

	out := make([]*ChainData, 0, len(c.chains))
	for _, v := range c.chains {
		out = append(out, &ChainData{
			Config:    v.Config,
			UpdatedAt: v.UpdatedAt,
		})
	}
	return out
}
