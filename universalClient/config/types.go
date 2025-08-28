package config

import (
	"time"
)

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
	PushChainGRPCURLs     []string      `json:"push_chain_grpc_urls"`    // Push Chain gRPC endpoints (default: ["localhost:9090"])
	ConfigRefreshInterval time.Duration `json:"config_refresh_interval"` // How often to refresh configs (default: 10m)
	MaxRetries            int           `json:"max_retries"`             // Max retry attempts for registry queries (default: 3)
	RetryBackoff          time.Duration `json:"retry_backoff"`           // Initial retry backoff duration (default: 1s)

	// Startup configuration
	InitialFetchRetries int           `json:"initial_fetch_retries"` // Number of retries for initial config fetch (default: 5)
	InitialFetchTimeout time.Duration `json:"initial_fetch_timeout"` // Timeout per initial fetch attempt (default: 30s)

	// Query Server configuration
	QueryServerPort int `json:"query_server_port"` // Port for HTTP query server (default: 8080)

	// Keyring configuration
	KeyringBackend KeyringBackend `json:"keyring_backend"` // Keyring backend type (file/test)
}
