package config

import "fmt"

// KeyringBackend represents the type of keyring backend to use
type KeyringBackend string

const (
	// KeyringBackendTest is the test Cosmos keyring backend (unencrypted)
	KeyringBackendTest KeyringBackend = "test"

	// KeyringBackendFile is the file Cosmos keyring backend (encrypted)
	KeyringBackendFile KeyringBackend = "file"
)

type Config struct {
	// Log Config
	LogLevel   int    `json:"log_level"`   // e.g., 0 = debug, 1 = info, etc.
	LogFormat  string `json:"log_format"`  // "json" or "console"
	LogSampler bool   `json:"log_sampler"` // if true, samples logs (e.g., 1 in 5)

	// Node Config
	NodeHome string `json:"node_home"` // Node home directory (default: ~/.puniversal)

	// Push Chain configuration
	PushChainID                  string   `json:"push_chain_id"`                   // Push Chain chain ID (default: localchain_9000-1)
	PushChainGRPCURLs            []string `json:"push_chain_grpc_urls"`            // Push Chain gRPC endpoints (default: ["localhost:9090"])
	PushValoperAddress           string   `json:"push_valoper_address"`            // Push Chain validator operator address (pushvaloper1...)
	ConfigRefreshIntervalSeconds int      `json:"config_refresh_interval_seconds"` // How often to refresh configs in seconds (default: 60)
	MaxRetries                   int      `json:"max_retries"`                     // Max retry attempts for registry queries (default: 3)

	// Query Server Config
	QueryServerPort int `json:"query_server_port"` // Port for HTTP query server (default: 8080)

	// Keyring configuration
	KeyringBackend  KeyringBackend `json:"keyring_backend"`  // Keyring backend type (file/test)
	KeyringPassword string         `json:"keyring_password"` // Password for file backend keyring encryption

	// Unified per-chain configuration
	ChainConfigs map[string]ChainSpecificConfig `json:"chain_configs"` // Map of chain ID to all chain-specific settings

	// TSS Node configuration
	TSSP2PPrivateKeyHex string `json:"tss_p2p_private_key_hex"` // Ed25519 private key in hex for libp2p identity
	TSSP2PListen        string `json:"tss_p2p_listen"`          // libp2p listen address (default: /ip4/0.0.0.0/tcp/39000)
	TSSPassword         string `json:"tss_password"`            // Encryption password for keyshares
	TSSHomeDir          string `json:"tss_home_dir"`            // Keyshare storage directory (default: ~/.puniversal/tss)
}

// ChainSpecificConfig holds all chain-specific configuration in one place
type ChainSpecificConfig struct {
	// RPC Configuration
	RPCURLs []string `json:"rpc_urls,omitempty"` // RPC endpoints for this chain

	// Transaction Cleanup Configuration
	CleanupIntervalSeconds *int `json:"cleanup_interval_seconds,omitempty"` // How often to run cleanup for this chain (required)
	RetentionPeriodSeconds *int `json:"retention_period_seconds,omitempty"` // How long to keep confirmed transactions for this chain (required)

	// Event Monitoring Configuration
	EventPollingIntervalSeconds *int `json:"event_polling_interval_seconds,omitempty"` // How often to poll for new events for this chain (required)

	// Event Start Cursor
	// If set to a non-negative value, gateway event watchers start from this
	// block/slot for this chain. If set to -1 or not present, start from the
	// latest block/slot (or from DB resume point when available).
	EventStartFrom *int64 `json:"event_start_from,omitempty"`

	// Gas Oracle Configuration
	GasPriceIntervalSeconds *int `json:"gas_price_interval_seconds,omitempty"` // How often to fetch and vote on gas price (default: 30 seconds)

	// Future chain-specific settings can be added here
}

// GetChainCleanupSettings returns cleanup settings for a specific chain
// Returns chain-specific settings (required per chain)
func (c *Config) GetChainCleanupSettings(chainID string) (cleanupInterval, retentionPeriod int, err error) {
	if c.ChainConfigs == nil {
		return 0, 0, fmt.Errorf("no chain configs found")
	}

	config, ok := c.ChainConfigs[chainID]
	if !ok {
		return 0, 0, fmt.Errorf("no config found for chain %s", chainID)
	}

	if config.CleanupIntervalSeconds == nil {
		return 0, 0, fmt.Errorf("cleanup_interval_seconds is required for chain %s", chainID)
	}
	if config.RetentionPeriodSeconds == nil {
		return 0, 0, fmt.Errorf("retention_period_seconds is required for chain %s", chainID)
	}

	return *config.CleanupIntervalSeconds, *config.RetentionPeriodSeconds, nil
}

// GetChainConfig returns the complete configuration for a specific chain
func (c *Config) GetChainConfig(chainID string) *ChainSpecificConfig {
	if c.ChainConfigs != nil {
		if config, ok := c.ChainConfigs[chainID]; ok {
			return &config
		}
	}
	// Return empty config if not found
	return &ChainSpecificConfig{}
}
