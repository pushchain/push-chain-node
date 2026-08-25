package config

import (
	"fmt"
	"strings"
)

// KeyringBackend represents the type of keyring backend to use.
type KeyringBackend string

const (
	KeyringBackendTest KeyringBackend = "test"
	KeyringBackendFile KeyringBackend = "file"
)

const NetworkTestnet = "testnet"

// IsTestnet reports whether this node is on testnet. Any other value, including
// unset, is treated as mainnet.
func (c *Config) IsTestnet() bool {
	return strings.EqualFold(strings.TrimSpace(c.PushNetwork), NetworkTestnet)
}

// AllowsZeroConfirmationsForChain reports whether a registry confirmation depth
// of 0 is honored for this chain instead of falling back to a safe depth.
//
// A registry 0 is ambiguous: proto3 encodes a deliberate 0 and an unset field
// identically, so it cannot be read as "instant finality" on its own. Honoring
// it therefore requires an out-of-band signal, either a testnet deployment or an
// explicit per-chain declaration. Anything else falls back.
func (c *Config) AllowsZeroConfirmationsForChain(chainID string) bool {
	if c.IsTestnet() {
		return true
	}
	return c.GetChainConfig(chainID).InstantFinality
}

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

	// PushNetwork is "mainnet" or "testnet"; unset/unknown is treated as mainnet.
	PushNetwork string `json:"push_network"`

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
	GasPriceMarkupPercent       *int              `json:"gas_price_markup_percent,omitempty"` // % markup on fetched gas price to handle spikes
	ProtocolALT                 string            `json:"protocol_alt,omitempty"`             // Protocol ALT address (base58) for V0 transactions
	TokenALTs                   map[string]string `json:"token_alts,omitempty"`               // mint address → token ALT address (base58)

	// InstantFinality declares that this chain's source finality is immediate, so
	// a registry confirmation depth of 0 is honored rather than replaced by a
	// safe default. Required because proto3 cannot distinguish a deliberate 0
	// from an unset field, so the intent has to be stated out of band.
	InstantFinality bool `json:"instant_finality,omitempty"`

	// SVM rent reclaimer (orphaned StoredIxData PDA cleanup). Both default if unset.
	RentReclaimSweepIntervalSeconds *int `json:"rent_reclaim_sweep_interval_seconds,omitempty"` // how often to sweep
	RentReclaimMinPDAAgeSeconds     *int `json:"rent_reclaim_min_pda_age_seconds,omitempty"`    // skip PDAs younger than this
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
