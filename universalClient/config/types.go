package config

import "time"

type Config struct {
	// Log Config
	LogLevel   int    `json:"log_level"`   // e.g., 0 = debug, 1 = info, etc.
	LogFormat  string `json:"log_format"`  // "json" or "console"
	LogSampler bool   `json:"log_sampler"` // if true, samples logs (e.g., 1 in 5)

	// Registry Config
	PushChainGRPCURLs     []string      `json:"push_chain_grpc_urls"`    // Push Chain gRPC endpoints (default: ["localhost:9090"])
	ConfigRefreshInterval time.Duration `json:"config_refresh_interval"` // How often to refresh configs (default: 10m)
	MaxRetries            int           `json:"max_retries"`             // Max retry attempts for registry queries (default: 3)
	RetryBackoff          time.Duration `json:"retry_backoff"`           // Initial retry backoff duration (default: 1s)

	// Startup configuration
	InitialFetchRetries int           `json:"initial_fetch_retries"` // Number of retries for initial config fetch (default: 5)
	InitialFetchTimeout time.Duration `json:"initial_fetch_timeout"` // Timeout per initial fetch attempt (default: 30s)

	QueryServerPort     int           `json:"query_server_port"`        // Port for HTTP query server (default: 8080)
	
	// Event monitoring configuration
	EventPollingInterval time.Duration `json:"event_polling_interval"`   // How often to poll for new events (default: 5s)

	// Transaction cleanup configuration
	TransactionCleanupInterval  time.Duration `json:"transaction_cleanup_interval"`  // How often to run cleanup (default: 1h)
	TransactionRetentionPeriod  time.Duration `json:"transaction_retention_period"`  // How long to keep confirmed transactions (default: 24h)

	// RPC Pool configuration
	ChainRPCURLs  map[string][]string `json:"chain_rpc_urls"`  // Map of chain ID to array of RPC URLs
	RPCPoolConfig RPCPoolConfig       `json:"rpc_pool_config"` // RPC pool configuration
}

// RPCPoolConfig holds configuration for RPC endpoint pooling
type RPCPoolConfig struct {
	HealthCheckInterval   time.Duration `json:"health_check_interval"`   // How often to check endpoint health (default: 30s)
	UnhealthyThreshold    int           `json:"unhealthy_threshold"`     // Consecutive failures before marking unhealthy (default: 3)
	RecoveryInterval      time.Duration `json:"recovery_interval"`       // How long to wait before retesting excluded endpoint (default: 5m)
	MinHealthyEndpoints   int           `json:"min_healthy_endpoints"`   // Minimum healthy endpoints required (default: 1)
	RequestTimeout        time.Duration `json:"request_timeout"`         // Timeout for individual RPC requests (default: 10s)
	LoadBalancingStrategy string        `json:"load_balancing_strategy"` // "round-robin" or "weighted" (default: "round-robin")
}
