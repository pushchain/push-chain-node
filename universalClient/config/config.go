package config

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/rollchains/pchain/universalClient/constant"
)

const (
	configSubdir   = "config"
	configFileName = "pushuv_config.json"
)

//go:embed default_config.json
var defaultConfigJSON []byte

// LoadDefaultConfig loads the default configuration from the embedded JSON
func LoadDefaultConfig() (Config, error) {
	var cfg Config
	if err := json.Unmarshal(defaultConfigJSON, &cfg); err != nil {
		return Config{}, fmt.Errorf("failed to unmarshal default config: %w", err)
	}
	
	// Validate the config
	if err := validateConfig(&cfg); err != nil {
		return Config{}, fmt.Errorf("invalid default config: %w", err)
	}
	
	return cfg, nil
}

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
		// Default to localhost (clean base URL without port) if no URLs provided
		cfg.PushChainGRPCURLs = []string{"localhost"}
	}

	// Set defaults for query server
	if cfg.QueryServerPort == 0 {
		cfg.QueryServerPort = 8080
	}

	// Set defaults and validate hot key management config
	// Don't override if already set, just validate
	if cfg.KeyringBackend != "" {
		// Validate keyring backend
		if cfg.KeyringBackend != KeyringBackendFile && cfg.KeyringBackend != KeyringBackendTest {
			// Try to fix common case issues
			if cfg.KeyringBackend == "test" || cfg.KeyringBackend == KeyringBackend("test") {
				cfg.KeyringBackend = KeyringBackendTest
			} else if cfg.KeyringBackend == "file" || cfg.KeyringBackend == KeyringBackend("file") {
				cfg.KeyringBackend = KeyringBackendFile
			} else {
				return fmt.Errorf("keyring backend must be 'file' or 'test', got: %s", cfg.KeyringBackend)
			}
		}
	} else {
		// Default to test backend for local development
		cfg.KeyringBackend = KeyringBackendTest
	}
	
	// Set default for key check interval
	if cfg.KeyCheckInterval == 0 {
		cfg.KeyCheckInterval = 30 // Default to 30 seconds
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
	
	// Don't validate for now - let the config file values pass through
	// if err := validateConfig(&cfg); err != nil {
	//	return Config{}, fmt.Errorf("invalid config: %w", err)
	// }
	
	return cfg, nil
}


// GetKeyringDir returns the full path to the keyring directory
func GetKeyringDir(cfg *Config) string {
	return filepath.Join(constant.DefaultNodeHome, "keys")
}
