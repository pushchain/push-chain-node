package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	configSubdir   = "config"
	configFileName = "pushuv_config.json"
)

func validateConfig(cfg *Config) error {
	// Validate log level
	if cfg.LogLevel < 0 || cfg.LogLevel > 5 {
		return fmt.Errorf("log level must be between 0 and 5")
	}

	// Validate log format
	if cfg.LogFormat != "json" && cfg.LogFormat != "console" {
		return fmt.Errorf("log format must be 'json' or 'console'")
	}

	// Set defaults for registry config
	if cfg.ConfigRefreshInterval == 0 {
		cfg.ConfigRefreshInterval = 60 * time.Second
	}
	if cfg.MaxRetries == 0 {
		cfg.MaxRetries = 3
	}
	if cfg.RetryBackoff == 0 {
		cfg.RetryBackoff = time.Second
	}

	// Set defaults for startup config
	if cfg.InitialFetchRetries == 0 {
		cfg.InitialFetchRetries = 5
	}
	if cfg.InitialFetchTimeout == 0 {
		cfg.InitialFetchTimeout = 30 * time.Second
	}

	// Validate registry config
	if len(cfg.PushChainGRPCURLs) == 0 {
		// Default to localhost:9090 if no URLs provided
		cfg.PushChainGRPCURLs = []string{"localhost:9090"}
	}

	// Set defaults for query server
	if cfg.QueryServerPort == 0 {
		cfg.QueryServerPort = 8080
	}

	// Set defaults for event monitoring
	if cfg.EventPollingInterval == 0 {
		cfg.EventPollingInterval = 5 * time.Second
	}

	// Set defaults for transaction cleanup
	if cfg.TransactionCleanupInterval == 0 {
		cfg.TransactionCleanupInterval = time.Hour
	}
	if cfg.TransactionRetentionPeriod == 0 {
		cfg.TransactionRetentionPeriod = 24 * time.Hour
	}

	// Initialize ChainRPCURLs if nil
	if cfg.ChainRPCURLs == nil {
		cfg.ChainRPCURLs = make(map[string][]string)
	}

	// Set defaults for RPC pool config
	if cfg.RPCPoolConfig.HealthCheckInterval == 0 {
		cfg.RPCPoolConfig.HealthCheckInterval = 30 * time.Second
	}
	if cfg.RPCPoolConfig.UnhealthyThreshold == 0 {
		cfg.RPCPoolConfig.UnhealthyThreshold = 3
	}
	if cfg.RPCPoolConfig.RecoveryInterval == 0 {
		cfg.RPCPoolConfig.RecoveryInterval = 5 * time.Minute
	}
	if cfg.RPCPoolConfig.MinHealthyEndpoints == 0 {
		cfg.RPCPoolConfig.MinHealthyEndpoints = 1
	}
	if cfg.RPCPoolConfig.RequestTimeout == 0 {
		cfg.RPCPoolConfig.RequestTimeout = 10 * time.Second
	}
	if cfg.RPCPoolConfig.LoadBalancingStrategy == "" {
		cfg.RPCPoolConfig.LoadBalancingStrategy = "round-robin"
	}

	// Validate load balancing strategy
	if cfg.RPCPoolConfig.LoadBalancingStrategy != "round-robin" && 
		cfg.RPCPoolConfig.LoadBalancingStrategy != "weighted" {
		return fmt.Errorf("load balancing strategy must be 'round-robin' or 'weighted'")
	}

	return nil
}

// Save writes the given config to <NodeDir>/config/pushuv_config.json.
func Save(cfg *Config, basePath string) error {
	if err := validateConfig(cfg); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	configDir := filepath.Join(basePath, configSubdir)
	if err := os.MkdirAll(configDir, 0o750); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	configFile := filepath.Join(configDir, configFileName)
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(configFile, data, 0o600); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}
	return nil
}

// Load reads and returns the config from <BasePath>/config/pushuv_config.json.
func Load(basePath string) (Config, error) {
	configFile := filepath.Join(basePath, configSubdir, configFileName)
	data, err := os.ReadFile(filepath.Clean(configFile))
	if err != nil {
		return Config{}, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("failed to unmarshal config: %w", err)
	}
	return cfg, nil
}
