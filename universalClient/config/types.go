package config

import "fmt"

// KeyringBackend represents the type of keyring backend to use.
type KeyringBackend string

const (
	KeyringBackendTest KeyringBackend = "test"
	KeyringBackendFile KeyringBackend = "file"
)

// Config holds all configuration for the Universal Validator.
type Config struct {
	// Logging
	LogLevel   int    `json:"log_level"`
	LogFormat  string `json:"log_format"`
	LogSampler bool   `json:"log_sampler"`

	// Node
	NodeHome string `json:"node_home"`

	// Push Chain
	PushChainID                  string   `json:"push_chain_id"`
	PushChainGRPCURLs            []string `json:"push_chain_grpc_urls"`
	PushValoperAddress           string   `json:"push_valoper_address"`
	ConfigRefreshIntervalSeconds int      `json:"config_refresh_interval_seconds"`
	MaxRetries                   int      `json:"max_retries"`

	// Query Server
	QueryServerPort int `json:"query_server_port"`

	// Keyring
	KeyringBackend  KeyringBackend `json:"keyring_backend"`
	KeyringPassword string         `json:"keyring_password"`

	// Per-chain settings (keyed by CAIP-2 chain ID)
	ChainConfigs map[string]ChainSpecificConfig `json:"chain_configs"`

	// TSS
	TSSP2PPrivateKeyHex string `json:"tss_p2p_private_key_hex"`
	TSSP2PListen        string `json:"tss_p2p_listen"`
	TSSPassword         string `json:"tss_password"`
	TSSHomeDir          string `json:"tss_home_dir"`
}

// ChainSpecificConfig holds per-chain configuration.
type ChainSpecificConfig struct {
	RPCURLs                     []string          `json:"rpc_urls,omitempty"`
	CleanupIntervalSeconds      *int              `json:"cleanup_interval_seconds,omitempty"`
	RetentionPeriodSeconds      *int              `json:"retention_period_seconds,omitempty"`
	EventPollingIntervalSeconds *int              `json:"event_polling_interval_seconds,omitempty"`
	EventStartFrom              *int64            `json:"event_start_from,omitempty"`
	GasPriceIntervalSeconds     *int              `json:"gas_price_interval_seconds,omitempty"`
	GasPriceMarkupPercent       *int              `json:"gas_price_markup_percent,omitempty"`    // % markup on fetched gas price to handle spikes
	ProtocolALT                 string            `json:"protocol_alt,omitempty"`            // Protocol ALT address (base58) for V0 transactions
	TokenALTs                   map[string]string `json:"token_alts,omitempty"`              // mint address → token ALT address (base58)
}

// GetChainCleanupSettings returns cleanup settings for a specific chain.
func (c *Config) GetChainCleanupSettings(chainID string) (cleanupInterval, retentionPeriod int, err error) {
	cc, ok := c.ChainConfigs[chainID]
	if !ok {
		return 0, 0, fmt.Errorf("no config found for chain %s", chainID)
	}
	if cc.CleanupIntervalSeconds == nil {
		return 0, 0, fmt.Errorf("cleanup_interval_seconds is required for chain %s", chainID)
	}
	if cc.RetentionPeriodSeconds == nil {
		return 0, 0, fmt.Errorf("retention_period_seconds is required for chain %s", chainID)
	}
	return *cc.CleanupIntervalSeconds, *cc.RetentionPeriodSeconds, nil
}

// GetChainConfig returns the configuration for a specific chain, or an empty config if not found.
func (c *Config) GetChainConfig(chainID string) *ChainSpecificConfig {
	if cc, ok := c.ChainConfigs[chainID]; ok {
		return &cc
	}
	return &ChainSpecificConfig{}
}
