package registry

import (
	"sync"
	"time"

	"github.com/rs/zerolog"
	uregistrytypes "github.com/rollchains/pchain/x/uregistry/types"
)

// ChainData represents cached data for a single chain
type ChainData struct {
	Config    *uregistrytypes.ChainConfig
	Tokens    map[string]*uregistrytypes.TokenConfig // key: token address
	UpdatedAt time.Time
}

// ConfigCache provides thread-safe caching for chain and token configurations
type ConfigCache struct {
	chains     map[string]*ChainData // key: CAIP-2 chain ID
	mu         sync.RWMutex
	lastUpdate time.Time
	logger     zerolog.Logger
}

// NewConfigCache creates a new configuration cache
func NewConfigCache(logger zerolog.Logger) *ConfigCache {
	return &ConfigCache{
		chains: make(map[string]*ChainData),
		logger: logger.With().Str("component", "config_cache").Logger(),
	}
}

// GetChainConfig retrieves a chain configuration from cache
func (c *ConfigCache) GetChainConfig(chainID string) *uregistrytypes.ChainConfig {
	c.mu.RLock()
	defer c.mu.RUnlock()

	chainData, exists := c.chains[chainID]
	if exists && chainData != nil {
		c.logger.Debug().
			Str("chain_id", chainID).
			Msg("cache hit for chain config")
		return chainData.Config
	}

	c.logger.Debug().
		Str("chain_id", chainID).
		Msg("cache miss for chain config")
	return nil
}

// GetAllChainConfigs returns all cached chain configurations
func (c *ConfigCache) GetAllChainConfigs() []*uregistrytypes.ChainConfig {
	c.mu.RLock()
	defer c.mu.RUnlock()

	configs := make([]*uregistrytypes.ChainConfig, 0, len(c.chains))
	for _, chainData := range c.chains {
		if chainData != nil && chainData.Config != nil {
			configs = append(configs, chainData.Config)
		}
	}

	c.logger.Debug().
		Int("count", len(configs)).
		Msg("returning all chain configs from cache")

	return configs
}

// GetTokenConfig retrieves a token configuration from cache
func (c *ConfigCache) GetTokenConfig(chain, address string) *uregistrytypes.TokenConfig {
	c.mu.RLock()
	defer c.mu.RUnlock()

	chainData, exists := c.chains[chain]
	if !exists || chainData == nil {
		c.logger.Debug().
			Str("chain", chain).
			Str("address", address).
			Msg("cache miss for token config - chain not found")
		return nil
	}

	tokenConfig, exists := chainData.Tokens[address]
	if exists {
		c.logger.Debug().
			Str("chain", chain).
			Str("address", address).
			Msg("cache hit for token config")
	} else {
		c.logger.Debug().
			Str("chain", chain).
			Str("address", address).
			Msg("cache miss for token config")
	}

	return tokenConfig
}

// GetTokenConfigsByChain returns all token configurations for a specific chain
func (c *ConfigCache) GetTokenConfigsByChain(chain string) []*uregistrytypes.TokenConfig {
	c.mu.RLock()
	defer c.mu.RUnlock()

	chainData, exists := c.chains[chain]
	if !exists || chainData == nil || chainData.Tokens == nil {
		c.logger.Debug().
			Str("chain", chain).
			Int("count", 0).
			Msg("returning empty token configs - chain not found")
		return []*uregistrytypes.TokenConfig{}
	}

	configs := make([]*uregistrytypes.TokenConfig, 0, len(chainData.Tokens))
	for _, tokenConfig := range chainData.Tokens {
		configs = append(configs, tokenConfig)
	}

	c.logger.Debug().
		Str("chain", chain).
		Int("count", len(configs)).
		Msg("returning token configs for chain from cache")

	return configs
}

// GetAllTokenConfigs returns all cached token configurations
func (c *ConfigCache) GetAllTokenConfigs() []*uregistrytypes.TokenConfig {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Calculate total token count
	totalCount := 0
	for _, chainData := range c.chains {
		if chainData != nil && chainData.Tokens != nil {
			totalCount += len(chainData.Tokens)
		}
	}

	configs := make([]*uregistrytypes.TokenConfig, 0, totalCount)
	for _, chainData := range c.chains {
		if chainData != nil && chainData.Tokens != nil {
			for _, tokenConfig := range chainData.Tokens {
				configs = append(configs, tokenConfig)
			}
		}
	}

	c.logger.Debug().
		Int("count", len(configs)).
		Msg("returning all token configs from cache")

	return configs
}

// UpdateChainConfigs updates the cached chain configurations
func (c *ConfigCache) UpdateChainConfigs(configs []*uregistrytypes.ChainConfig) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Preserve existing token data where chains still exist
	newChains := make(map[string]*ChainData)

	for _, config := range configs {
		if config != nil && config.Chain != "" {
			chainData := &ChainData{
				Config:    config,
				Tokens:    make(map[string]*uregistrytypes.TokenConfig),
				UpdatedAt: time.Now(),
			}

			// Preserve existing tokens if the chain already exists
			if existingChain, exists := c.chains[config.Chain]; exists && existingChain != nil && existingChain.Tokens != nil {
				chainData.Tokens = existingChain.Tokens
			}

			newChains[config.Chain] = chainData
		}
	}

	c.chains = newChains

	c.logger.Info().
		Int("count", len(configs)).
		Msg("updated chain configs in cache")
}

// UpdateTokenConfigs updates the cached token configurations
func (c *ConfigCache) UpdateTokenConfigs(configs []*uregistrytypes.TokenConfig) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Clear all existing tokens first
	for _, chainData := range c.chains {
		if chainData != nil {
			chainData.Tokens = make(map[string]*uregistrytypes.TokenConfig)
		}
	}

	// Add tokens to their respective chains
	tokenCount := 0
	for _, config := range configs {
		if config != nil && config.Chain != "" && config.Address != "" {
			chainData, exists := c.chains[config.Chain]
			if !exists {
				// Create chain data if it doesn't exist
				chainData = &ChainData{
					Config:    nil, // No chain config available
					Tokens:    make(map[string]*uregistrytypes.TokenConfig),
					UpdatedAt: time.Now(),
				}
				c.chains[config.Chain] = chainData
			}
			chainData.Tokens[config.Address] = config
			tokenCount++
		}
	}

	c.logger.Info().
		Int("count", tokenCount).
		Msg("updated token configs in cache")
}

// UpdateAll atomically updates both chain and token configurations
func (c *ConfigCache) UpdateAll(chains []*uregistrytypes.ChainConfig, tokens []*uregistrytypes.TokenConfig) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Build new hierarchical structure
	newChains := make(map[string]*ChainData)

	// First, add all chains
	for _, chainConfig := range chains {
		if chainConfig != nil && chainConfig.Chain != "" {
			newChains[chainConfig.Chain] = &ChainData{
				Config:    chainConfig,
				Tokens:    make(map[string]*uregistrytypes.TokenConfig),
				UpdatedAt: time.Now(),
			}
		}
	}

	// Then, add all tokens to their respective chains
	tokenCount := 0
	for _, tokenConfig := range tokens {
		if tokenConfig != nil && tokenConfig.Chain != "" && tokenConfig.Address != "" {
			chainData, exists := newChains[tokenConfig.Chain]
			if !exists {
				// Create chain data if it doesn't exist (chain config missing)
				chainData = &ChainData{
					Config:    nil,
					Tokens:    make(map[string]*uregistrytypes.TokenConfig),
					UpdatedAt: time.Now(),
				}
				newChains[tokenConfig.Chain] = chainData
			}
			chainData.Tokens[tokenConfig.Address] = tokenConfig
			tokenCount++
		}
	}

	c.chains = newChains
	c.lastUpdate = time.Now()

	c.logger.Info().
		Int("chains", len(chains)).
		Int("tokens", tokenCount).
		Time("updated_at", c.lastUpdate).
		Msg("updated all configs in cache")
}


// GetLastUpdate returns the last update timestamp
func (c *ConfigCache) GetLastUpdate() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.lastUpdate
}

// GetChainData returns the complete chain data including tokens
func (c *ConfigCache) GetChainData(chainID string) *ChainData {
	c.mu.RLock()
	defer c.mu.RUnlock()

	chainData, exists := c.chains[chainID]
	if !exists {
		return nil
	}

	// Return a copy to prevent external modification
	result := &ChainData{
		Config:    chainData.Config,
		UpdatedAt: chainData.UpdatedAt,
		Tokens:    make(map[string]*uregistrytypes.TokenConfig),
	}

	// Copy tokens
	for addr, token := range chainData.Tokens {
		result.Tokens[addr] = token
	}

	return result
}

