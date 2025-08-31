package config

type Config struct {
	// Log Config
	LogLevel   int    `json:"log_level"`   // e.g., 0 = debug, 1 = info, etc.
	LogFormat  string `json:"log_format"`  // "json" or "console"
	LogSampler bool   `json:"log_sampler"` // if true, samples logs (e.g., 1 in 5)

	// Registry Config
	PushChainGRPCURLs             []string `json:"push_chain_grpc_urls"`              // Push Chain gRPC endpoints (default: ["localhost:9090"])
	ConfigRefreshIntervalSeconds  int      `json:"config_refresh_interval_seconds"`   // How often to refresh configs in seconds (default: 60)
	MaxRetries                    int      `json:"max_retries"`                       // Max retry attempts for registry queries (default: 3)
	RetryBackoffSeconds           int      `json:"retry_backoff_seconds"`             // Initial retry backoff duration in seconds (default: 1)

	// Startup configuration
	InitialFetchRetries        int `json:"initial_fetch_retries"`         // Number of retries for initial config fetch (default: 5)
	InitialFetchTimeoutSeconds int `json:"initial_fetch_timeout_seconds"` // Timeout per initial fetch attempt in seconds (default: 30)

	QueryServerPort int `json:"query_server_port"` // Port for HTTP query server (default: 8080)
	
	// Event monitoring configuration
	EventPollingIntervalSeconds int `json:"event_polling_interval_seconds"` // How often to poll for new events in seconds (default: 5)

	// Database configuration
	DatabaseBaseDir string `json:"database_base_dir"` // Base directory for chain databases (default: ~/.puniversal/databases)

	// Transaction cleanup configuration
	TransactionCleanupIntervalSeconds int `json:"transaction_cleanup_interval_seconds"` // How often to run cleanup in seconds (default: 3600)
	TransactionRetentionPeriodSeconds int `json:"transaction_retention_period_seconds"` // How long to keep confirmed transactions in seconds (default: 86400)

	// RPC Pool configuration
	ChainRPCURLs  map[string][]string `json:"chain_rpc_urls"`  // Map of chain ID to array of RPC URLs
	RPCPoolConfig RPCPoolConfig       `json:"rpc_pool_config"` // RPC pool configuration
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

// GetChainRPCURLs returns the map of chain RPC URLs
func (c *Config) GetChainRPCURLs() map[string][]string {
	return c.ChainRPCURLs
}