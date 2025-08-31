package config

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const (
	configSubdir   = "config"
	configFileName = "pushuv_config.json"
)

//go:embed default_config.json
var defaultConfigJSON []byte

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
	if cfg.ConfigRefreshIntervalSeconds == 0 {
		cfg.ConfigRefreshIntervalSeconds = 60
	}
	if cfg.MaxRetries == 0 {
		cfg.MaxRetries = 3
	}
	if cfg.RetryBackoffSeconds == 0 {
		cfg.RetryBackoffSeconds = 1
	}

	// Set defaults for startup config
	if cfg.InitialFetchRetries == 0 {
		cfg.InitialFetchRetries = 5
	}
	if cfg.InitialFetchTimeoutSeconds == 0 {
		cfg.InitialFetchTimeoutSeconds = 30
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
	if cfg.EventPollingIntervalSeconds == 0 {
		cfg.EventPollingIntervalSeconds = 5
	}

	// Set defaults for transaction cleanup
	if cfg.TransactionCleanupIntervalSeconds == 0 {
		cfg.TransactionCleanupIntervalSeconds = 3600
	}
	if cfg.TransactionRetentionPeriodSeconds == 0 {
		cfg.TransactionRetentionPeriodSeconds = 86400
	}

	// Initialize ChainConfigs if nil or empty
	if cfg.ChainConfigs == nil || len(cfg.ChainConfigs) == 0 {
		// Load defaults from embedded config
		var defaultCfg Config
		if err := json.Unmarshal(defaultConfigJSON, &defaultCfg); err == nil {
			cfg.ChainConfigs = defaultCfg.ChainConfigs
		} else {
			cfg.ChainConfigs = make(map[string]ChainSpecificConfig)
		}
	}

	// Set defaults for RPC pool config
	if cfg.RPCPoolConfig.HealthCheckIntervalSeconds == 0 {
		cfg.RPCPoolConfig.HealthCheckIntervalSeconds = 30
	}
	if cfg.RPCPoolConfig.UnhealthyThreshold == 0 {
		cfg.RPCPoolConfig.UnhealthyThreshold = 3
	}
	if cfg.RPCPoolConfig.RecoveryIntervalSeconds == 0 {
		cfg.RPCPoolConfig.RecoveryIntervalSeconds = 300
	}
	if cfg.RPCPoolConfig.MinHealthyEndpoints == 0 {
		cfg.RPCPoolConfig.MinHealthyEndpoints = 1
	}
	if cfg.RPCPoolConfig.RequestTimeoutSeconds == 0 {
		cfg.RPCPoolConfig.RequestTimeoutSeconds = 10
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

// LoadDefaultConfig loads the default configuration from embedded JSON
func LoadDefaultConfig() (*Config, error) {
	var cfg Config
	if err := json.Unmarshal(defaultConfigJSON, &cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal default config: %w", err)
	}
	return &cfg, nil
}
