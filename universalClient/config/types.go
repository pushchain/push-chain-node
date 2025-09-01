package config

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

	// Registry Config
	PushChainGRPCURLs            []string `json:"push_chain_grpc_urls"`            // Push Chain gRPC endpoints (default: ["localhost:9090"])
	ConfigRefreshIntervalSeconds int      `json:"config_refresh_interval_seconds"` // How often to refresh configs in seconds (default: 60)
	MaxRetries                   int      `json:"max_retries"`                     // Max retry attempts for registry queries (default: 3)
	RetryBackoffSeconds          int      `json:"retry_backoff_seconds"`           // Initial retry backoff duration in seconds (default: 1)

	// Startup configuration
	InitialFetchRetries        int `json:"initial_fetch_retries"`         // Number of retries for initial config fetch (default: 5)
	InitialFetchTimeoutSeconds int `json:"initial_fetch_timeout_seconds"` // Timeout per initial fetch attempt in seconds (default: 30)

	QueryServerPort int `json:"query_server_port"` // Port for HTTP query server (default: 8080)

	// Keyring configuration
	KeyringBackend KeyringBackend `json:"keyring_backend"` // Keyring backend type (file/test)

	// Event monitoring configuration
	EventPollingIntervalSeconds int `json:"event_polling_interval_seconds"` // How often to poll for new events in seconds (default: 5)

	// Database configuration
	DatabaseBaseDir string `json:"database_base_dir"` // Base directory for chain databases (default: ~/.puniversal/databases)

	// Transaction cleanup configuration (global defaults)
	TransactionCleanupIntervalSeconds int `json:"transaction_cleanup_interval_seconds"` // Global default: How often to run cleanup in seconds (default: 3600)
	TransactionRetentionPeriodSeconds int `json:"transaction_retention_period_seconds"` // Global default: How long to keep confirmed transactions in seconds (default: 86400)

	// RPC Pool configuration
	RPCPoolConfig RPCPoolConfig `json:"rpc_pool_config"` // RPC pool configuration

	// Unified per-chain configuration
	ChainConfigs map[string]ChainSpecificConfig `json:"chain_configs"` // Map of chain ID to all chain-specific settings
}

// ChainSpecificConfig holds all chain-specific configuration in one place
type ChainSpecificConfig struct {
	// RPC Configuration
	RPCURLs []string `json:"rpc_urls,omitempty"` // RPC endpoints for this chain

	// Transaction Cleanup Configuration
	CleanupIntervalSeconds *int `json:"cleanup_interval_seconds,omitempty"` // How often to run cleanup for this chain (optional, uses global default if not set)
	RetentionPeriodSeconds *int `json:"retention_period_seconds,omitempty"` // How long to keep confirmed transactions for this chain (optional, uses global default if not set)

	// Future chain-specific settings can be added here
}

// RPCPoolConfig holds configuration for RPC endpoint pooling
type RPCPoolConfig struct {
	HealthCheckIntervalSeconds int    `json:"health_check_interval_seconds"` // How often to check endpoint health in seconds (default: 30)
	UnhealthyThreshold         int    `json:"unhealthy_threshold"`           // Consecutive failures before marking unhealthy (default: 3)
	RecoveryIntervalSeconds    int    `json:"recovery_interval_seconds"`     // How long to wait before retesting excluded endpoint in seconds (default: 300)
	MinHealthyEndpoints        int    `json:"min_healthy_endpoints"`         // Minimum healthy endpoints required (default: 1)
	RequestTimeoutSeconds      int    `json:"request_timeout_seconds"`       // Timeout for individual RPC requests in seconds (default: 10)
	LoadBalancingStrategy      string `json:"load_balancing_strategy"`       // "round-robin" or "weighted" (default: "round-robin")
}

// GetChainRPCURLs returns the map of chain RPC URLs extracted from unified config
func (c *Config) GetChainRPCURLs() map[string][]string {
	result := make(map[string][]string)
	if c.ChainConfigs != nil {
		for chainID, config := range c.ChainConfigs {
			if len(config.RPCURLs) > 0 {
				result[chainID] = config.RPCURLs
			}
		}
	}
	return result
}

// GetChainCleanupSettings returns cleanup settings for a specific chain
// Falls back to global defaults if no chain-specific settings exist
func (c *Config) GetChainCleanupSettings(chainID string) (cleanupInterval, retentionPeriod int) {
	// Start with global defaults
	cleanupInterval = c.TransactionCleanupIntervalSeconds
	retentionPeriod = c.TransactionRetentionPeriodSeconds

	// Check for chain-specific overrides in unified config
	if c.ChainConfigs != nil {
		if config, ok := c.ChainConfigs[chainID]; ok {
			// Override with chain-specific values if provided
			if config.CleanupIntervalSeconds != nil {
				cleanupInterval = *config.CleanupIntervalSeconds
			}
			if config.RetentionPeriodSeconds != nil {
				retentionPeriod = *config.RetentionPeriodSeconds
			}
		}
	}

	return cleanupInterval, retentionPeriod
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
